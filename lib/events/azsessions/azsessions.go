// Copyright 2022 Gravitational, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package azsessions

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/google/uuid"
	"github.com/gravitational/trace"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/session"
)

const (
	sessionContainerName    = "session"
	inprogressContainerName = "inprogress"

	// clientIDFragParam is the parameter in the fragment that specifies the optional client ID.
	clientIDFragParam = "azure_client_id"
)

// sessionName returns the name of the blob that contains the recording for a
// given session.
func sessionName(sid session.ID) string {
	return sid.String()
}

// uploadMarkerPrefix is the prefix of the names of the upload marker blobs.
// Listing blobs with this prefix will return an empty blob for each upload.
const uploadMarkerPrefix = "upload/"

// uploadMarkerName returns the blob name for the marker for a given upload.
func uploadMarkerName(upload events.StreamUpload) string {
	return fmt.Sprintf("%v%v/%v", uploadMarkerPrefix, upload.SessionID, upload.ID)
}

// partPrefix returns the prefix for the upload part blobs for a given upload.
// Listing blobs with this prefix will return all the parts that currently make
// up the upload.
func partPrefix(upload events.StreamUpload) string {
	return fmt.Sprintf("part/%v/%v/", upload.SessionID, upload.ID)
}

// partName returns the name of the blob for a specific part in an upload.
func partName(upload events.StreamUpload, partNumber int64) string {
	return fmt.Sprintf("%v%v", partPrefix(upload), partNumber)
}

// field names used for logging
const (
	fieldSessionID  = "session_id"
	fieldUploadID   = "upload_id"
	fieldPartNumber = "part"
	fieldPartCount  = "parts"
)

// Config is a struct of parameters to define the behavior of Handler.
type Config struct {
	// ServiceURL is the URL for the storage account to use.
	ServiceURL url.URL

	// ClientID, when set, defines the managed identity's client ID to use for
	// authentication.
	ClientID string

	// Log is the logger to use. If unset, it will default to the global logger
	// with a component of "azblob".
	Log logrus.FieldLogger
}

// SetFromURL sets values in Config based on the passed in URL: the fragment of
// the URL is parsed as if it was made out of query parameters, which define
// options for ourselves, and then the remainder of the URL is set as the
// service URL.
func (c *Config) SetFromURL(u *url.URL) error {
	if u == nil {
		return nil
	}

	c.ServiceURL = *u

	switch c.ServiceURL.Scheme {
	case teleport.SchemeAZBlob:
		c.ServiceURL.Scheme = "https"
	case teleport.SchemeAZBlobHTTP:
		c.ServiceURL.Scheme = "http"
	}

	params, err := url.ParseQuery(c.ServiceURL.EscapedFragment())
	if err != nil {
		return trace.Wrap(err)
	}
	c.ServiceURL.Fragment = ""
	c.ServiceURL.RawFragment = ""

	c.ClientID = params.Get(clientIDFragParam)

	return nil
}

func (c *Config) CheckAndSetDefaults() error {
	if c.Log == nil {
		c.Log = logrus.WithField(trace.Component, teleport.SchemeAZBlob)
	}

	return nil
}

func NewHandler(ctx context.Context, cfg Config) (*Handler, error) {
	if err := cfg.CheckAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}

	var cred azcore.TokenCredential
	if cfg.ClientID != "" {
		c, err := azidentity.NewManagedIdentityCredential(&azidentity.ManagedIdentityCredentialOptions{
			ID: azidentity.ClientID(cfg.ClientID),
		})
		if err != nil {
			return nil, trace.Wrap(err)
		}
		cred = c
	} else {
		c, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		cred = c
	}

	cred = &cachedTokenCredential{TokenCredential: cred}

	ensureContainer := func(name string) (*container.Client, error) {
		containerURL := cfg.ServiceURL
		containerURL.Path = name

		cntClient, err := container.NewClient(containerURL.String(), cred, nil)
		if err != nil {
			return nil, trace.Wrap(err)
		}

		_, err = cErr2(cntClient.GetProperties(ctx, nil))
		if err == nil {
			return cntClient, nil
		}
		if !trace.IsNotFound(err) && !trace.IsAccessDenied(err) {
			return nil, trace.Wrap(err)
		}

		cfg.Log.WithError(err).Debugf("Failed to confirm that the %v container exists, attempting creation.", name)
		// someone else might've created the container between GetProperties and
		// Create, so we ignore AlreadyExists
		_, err = cErr2(cntClient.Create(ctx, nil))
		if err == nil || trace.IsAlreadyExists(err) {
			return cntClient, nil
		}
		if trace.IsAccessDenied(err) {
			cfg.Log.WithError(err).Warnf(
				"Could not create the %v container, please ensure it exists or session recordings will not be stored correctly.", name)
			return cntClient, nil
		}
		return nil, trace.Wrap(err)
	}

	session, err := ensureContainer(sessionContainerName)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	inprogress, err := ensureContainer(inprogressContainerName)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return &Handler{c: cfg, cred: cred, session: session, inprogress: inprogress}, nil
}

// Handler is a MultipartHandler that stores data in Azure Blob Storage.
type Handler struct {
	c          Config
	cred       azcore.TokenCredential
	session    *container.Client
	inprogress *container.Client
}

var _ events.MultipartHandler = (*Handler)(nil)

// sessionBlob returns a BlockBlobClient for the blob of the recording of the
// session.
func (h *Handler) sessionBlob(sessionID session.ID) *blockblob.Client {
	return h.session.NewBlockBlobClient(sessionName(sessionID))
}

// uploadMarkerBlob returns a BlockBlobClient for the marker blob of the stream
// upload.
func (h *Handler) uploadMarkerBlob(upload events.StreamUpload) *blockblob.Client {
	return h.inprogress.NewBlockBlobClient(uploadMarkerName(upload))
}

// partBlob returns a BlockBlobClient for the blob of the part of the specified
// upload, with the given part number.
func (h *Handler) partBlob(upload events.StreamUpload, partNumber int64) *blockblob.Client {
	return h.inprogress.NewBlockBlobClient(partName(upload, partNumber))
}

// Upload implements events.UploadHandler
func (h *Handler) Upload(ctx context.Context, sessionID session.ID, reader io.Reader) (string, error) {
	blob := h.sessionBlob(sessionID)

	if _, err := cErr2(blob.UploadStream(ctx, reader, &blockblob.UploadStreamOptions{
		AccessConditions: &blobDoesNotExist,
	})); err != nil {
		return "", trace.Wrap(err)
	}
	h.c.Log.WithField(fieldSessionID, sessionID).Debug("Uploaded session.")

	return blob.URL(), nil
}

// Download implements events.UploadHandler
func (h *Handler) Download(ctx context.Context, sessionID session.ID, writer io.WriterAt) error {
	blob := h.sessionBlob(sessionID)
	const beginOffset = 0

	resp, err := blob.DownloadStream(ctx, &azblob.DownloadStreamOptions{
		Range: azblob.HTTPRange{
			Offset: beginOffset,
			Count:  blockblob.CountToEnd,
		},
	})
	if cerr := cErr(err); cerr != nil {
		return trace.Wrap(cerr)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.c.Log.WithError(err).WithField(fieldSessionID, sessionID).Warn("Error closing downloaded session blob.")
		}
	}()

	_, err = io.Copy(io.NewOffsetWriter(writer, beginOffset), resp.Body)
	if cerr := cErr(err); cerr != nil {
		return trace.Wrap(cerr)
	}

	h.c.Log.WithField(fieldSessionID, sessionID).Debug("Downloaded session.")
	return nil
}

// CreateUpload implements events.MultipartUploader
func (h *Handler) CreateUpload(ctx context.Context, sessionID session.ID) (*events.StreamUpload, error) {
	upload := events.StreamUpload{
		ID:        uuid.NewString(),
		SessionID: sessionID,
	}

	blob := h.uploadMarkerBlob(upload)

	emptyBody := streaming.NopCloser(&bytes.Reader{})
	if _, err := cErr2(blob.Upload(ctx, emptyBody, &blockblob.UploadOptions{
		AccessConditions: &blobDoesNotExist,
	})); err != nil {
		return nil, trace.Wrap(err)
	}
	h.c.Log.WithField(fieldSessionID, sessionID).Debug("Created upload marker.")

	return &upload, nil
}

// CompleteUpload implements events.MultipartUploader by composing the final
// session recording blob in the session container from the parts in the
// inprogress container, using the Put Block From URL API. Might take a little
// time, but doesn't require any data transfer.
func (h *Handler) CompleteUpload(ctx context.Context, upload events.StreamUpload, parts []events.StreamPart) error {
	blob := h.sessionBlob(upload.SessionID)
	markerBlob := h.uploadMarkerBlob(upload)

	// TODO(espadolini): explore the possibility of using leases to get
	// exclusive access while writing, and to guarantee that leftover parts are
	// cleaned up before a new attempt

	parts = slices.Clone(parts)
	slices.SortFunc(parts, func(a, b events.StreamPart) bool { return a.Number < b.Number })

	partURLs := make([]string, 0, len(parts))
	for _, part := range parts {
		b := h.partBlob(upload, part.Number)
		partURLs = append(partURLs, b.URL())
	}

	token, err := h.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://storage.azure.com/.default"},
	})
	if err != nil {
		return trace.Wrap(err)
	}
	copySourceAuthorization := "Bearer " + token.Token
	stageOptions := &blockblob.StageBlockFromURLOptions{
		CopySourceAuthorization: &copySourceAuthorization,
	}

	log := h.c.Log.WithFields(logrus.Fields{
		fieldSessionID: upload.SessionID,
		fieldUploadID:  upload.ID,
	})

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(5) // default parallelism as used by azblob.DoBatchTransfer

	log.WithField(fieldPartCount, len(parts)).Debug("Beginning upload completion.")
	blockNames := make([]string, len(parts))
	// TODO(espadolini): use stable names (upload id, part number and then some
	// hash maybe) to avoid re-staging parts more than once across multiple
	// completion attempts?
	for i := range parts {
		i := i
		eg.Go(func() error {
			// we use block names that are local to this function so we don't
			// interact with other ongoing uploads; trick copied from
			// (*BlockBlobClient).UploadBuffer and UploadFile
			u := uuid.New()
			blockNames[i] = base64.StdEncoding.EncodeToString(u[:])

			if _, err := cErr2(blob.StageBlockFromURL(egCtx, blockNames[i], partURLs[i], stageOptions)); err != nil {
				return trace.Wrap(err)
			}
			log.WithField(fieldPartNumber, i).Debug("Staged part.")
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return trace.Wrap(err)
	}

	log.Debug("Committing part list.")
	if _, err := cErr2(blob.CommitBlockList(ctx, blockNames, &blockblob.CommitBlockListOptions{
		AccessConditions: &blobDoesNotExist,
	})); err != nil {
		if !trace.IsAlreadyExists(err) {
			return trace.Wrap(err)
		}
		log.Warn("Session upload already exists, cleaning up marker.")
		parts = nil // don't delete parts that we didn't persist
	} else {
		log.Debug("Completed session upload.")
	}

	// TODO(espadolini): should the cleanup run in its own goroutine? What
	// should the cancellation context for the cleanup be in that case?
	if _, err := cErr2(markerBlob.Delete(ctx, nil)); err != nil && !trace.IsNotFound(err) {
		log.WithError(err).WithField(fieldPartCount, len(parts)).Warn("Failed to clean up upload marker.")
		return nil
	}

	// TODO(espadolini): group deletes together with Blob Batch, not supported
	// by the SDK
	for _, part := range parts {
		b := h.partBlob(upload, part.Number)
		if _, err := cErr2(b.Delete(ctx, nil)); err != nil {
			log.WithField(fieldPartNumber, part.Number).WithError(err).Warn("Failed to clean up part.")
		}
	}

	return nil
}

// ReserveUploadPart implements events.MultipartUploader by doing nothing.
func (*Handler) ReserveUploadPart(ctx context.Context, upload events.StreamUpload, partNumber int64) error {
	return nil
}

// UploadPart implements events.MultipartUploader
func (h *Handler) UploadPart(ctx context.Context, upload events.StreamUpload, partNumber int64, partBody io.ReadSeeker) (*events.StreamPart, error) {
	blob := h.partBlob(upload, partNumber)

	// our parts are just over 5 MiB (events.MinUploadPartSizeBytes) so we can
	// upload them in one shot
	if _, err := cErr2(blob.Upload(ctx, streaming.NopCloser(partBody), nil)); err != nil {
		return nil, trace.Wrap(err)
	}
	h.c.Log.WithFields(logrus.Fields{
		fieldSessionID:  upload.SessionID,
		fieldUploadID:   upload.ID,
		fieldPartNumber: partNumber,
	}).Debug("Uploaded part.")

	return &events.StreamPart{Number: partNumber}, nil
}

// ListParts implements events.MultipartUploader
func (h *Handler) ListParts(ctx context.Context, upload events.StreamUpload) ([]events.StreamPart, error) {
	prefix := partPrefix(upload)

	var parts []events.StreamPart
	pager := h.inprogress.NewListBlobsFlatPager(&azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if cerr := cErr(err); cerr != nil {
			return nil, trace.Wrap(cerr)
		}

		if resp.Segment == nil {
			continue
		}
		parts = slices.Grow(parts, len(resp.Segment.BlobItems))
		for _, b := range resp.Segment.BlobItems {
			if b == nil ||
				b.Name == nil ||
				!strings.HasPrefix(*b.Name, prefix) {
				continue
			}

			pn := strings.TrimPrefix(*b.Name, prefix)
			partNumber, err := strconv.ParseInt(pn, 10, 0)
			if err != nil {
				continue
			}

			parts = append(parts, events.StreamPart{Number: partNumber})
		}
	}

	slices.SortFunc(parts, func(a, b events.StreamPart) bool { return a.Number < b.Number })

	return parts, nil
}

// ListUploads implements events.MultipartUploader
func (h *Handler) ListUploads(ctx context.Context) ([]events.StreamUpload, error) {
	prefix := uploadMarkerPrefix
	var uploads []events.StreamUpload

	pager := h.inprogress.NewListBlobsFlatPager(&azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})
	for pager.More() {
		r, err := pager.NextPage(ctx)
		if cerr := cErr(err); cerr != nil {
			return nil, trace.Wrap(cerr)
		}

		if r.Segment == nil {
			continue
		}
		uploads = slices.Grow(uploads, len(r.Segment.BlobItems))
		for _, b := range r.Segment.BlobItems {
			if b == nil ||
				b.Name == nil ||
				!strings.HasPrefix(*b.Name, prefix) ||
				b.Properties == nil ||
				b.Properties.CreationTime == nil {
				continue
			}

			name := strings.TrimPrefix(*b.Name, prefix)
			sid, uid, ok := strings.Cut(name, "/")
			if !ok {
				continue
			}
			if _, err := session.ParseID(sid); err != nil {
				continue
			}
			if _, err := uuid.Parse(uid); err != nil {
				continue
			}

			uploads = append(uploads, events.StreamUpload{
				ID:        uid,
				SessionID: session.ID(sid),
				Initiated: *b.Properties.CreationTime,
			})
		}
	}

	slices.SortFunc(uploads, func(a, b events.StreamUpload) bool { return a.Initiated.Before(b.Initiated) })

	return uploads, nil
}

// GetUploadMetadata implements events.MultipartUploader
func (h *Handler) GetUploadMetadata(sessionID session.ID) events.UploadMetadata {
	url := h.c.ServiceURL
	url.Path = path.Join(url.Path, sessionContainerName, sessionID.String())

	return events.UploadMetadata{
		URL:       url.String(),
		SessionID: sessionID,
	}
}