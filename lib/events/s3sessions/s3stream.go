/*
 * Teleport
 * Copyright (C) 2023  Gravitational, Inc.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package s3sessions

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/gravitational/trace"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/session"
	awsutils "github.com/gravitational/teleport/lib/utils/aws"
)

// CreateUpload creates a multipart upload
func (h *Handler) CreateUpload(ctx context.Context, sessionID session.ID) (*events.StreamUpload, error) {
	start := time.Now()
	defer func() { h.Infof("Upload created in %v.", time.Since(start)) }()

	input := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(h.Bucket),
		Key:    aws.String(h.path(sessionID)),
	}
	if !h.Config.DisableServerSideEncryption {
		input.ServerSideEncryption = aws.String(s3.ServerSideEncryptionAwsKms)

		if h.Config.SSEKMSKey != "" {
			input.SSEKMSKeyId = aws.String(h.Config.SSEKMSKey)
		}
	}
	if h.Config.ACL != "" {
		input.ACL = aws.String(h.Config.ACL)
	}

	resp, err := h.client.CreateMultipartUploadWithContext(ctx, input)
	if err != nil {
		return nil, awsutils.ConvertS3Error(err)
	}

	return &events.StreamUpload{SessionID: sessionID, ID: *resp.UploadId}, nil
}

// UploadPart uploads part
func (h *Handler) UploadPart(ctx context.Context, upload events.StreamUpload, partNumber int64, partBody io.ReadSeeker) (*events.StreamPart, error) {
	start := time.Now()
	defer func() {
		h.Infof("UploadPart(upload %v) part(%v) session(%v) uploaded in %v.", upload.ID, partNumber, upload.SessionID, time.Since(start))
	}()

	// This upload exceeded maximum number of supported parts, error now.
	if partNumber > s3manager.MaxUploadParts {
		return nil, trace.LimitExceeded(
			"exceeded total allowed S3 limit MaxUploadParts (%d). Adjust PartSize to fit in this limit", s3manager.MaxUploadParts)
	}

	params := &s3.UploadPartInput{
		Bucket:     aws.String(h.Bucket),
		UploadId:   aws.String(upload.ID),
		Key:        aws.String(h.path(upload.SessionID)),
		Body:       partBody,
		PartNumber: aws.Int64(partNumber),
	}

	resp, err := h.client.UploadPartWithContext(ctx, params)
	if err != nil {
		return nil, awsutils.ConvertS3Error(err)
	}

	return &events.StreamPart{ETag: *resp.ETag, Number: partNumber}, nil
}

func (h *Handler) abortUpload(ctx context.Context, upload events.StreamUpload) error {
	req := &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(h.Bucket),
		Key:      aws.String(h.path(upload.SessionID)),
		UploadId: aws.String(upload.ID),
	}
	_, err := h.client.AbortMultipartUploadWithContext(ctx, req)
	if err != nil {
		return awsutils.ConvertS3Error(err)
	}
	return nil
}

// maxPartsPerUpload is the maximum number of parts for a single multipart upload.
// See https://docs.aws.amazon.com/AmazonS3/latest/userguide/qfacts.html
const maxPartsPerUpload = 10000

// CompleteUpload completes the upload
func (h *Handler) CompleteUpload(ctx context.Context, upload events.StreamUpload, parts []events.StreamPart) error {
	if len(parts) == 0 {
		return h.abortUpload(ctx, upload)
	}
	if len(parts) > maxPartsPerUpload {
		return trace.BadParameter("too many parts for a single S3 upload (%d), "+
			"must be less than %d", len(parts), maxPartsPerUpload)
	}

	start := time.Now()
	defer func() { h.Infof("UploadPart(%v) completed in %v.", upload.ID, time.Since(start)) }()

	// Parts must be sorted in PartNumber order.
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].Number < parts[j].Number
	})

	completedParts := make([]*s3.CompletedPart, len(parts))
	for i := range parts {
		completedParts[i] = &s3.CompletedPart{
			ETag:       aws.String(parts[i].ETag),
			PartNumber: aws.Int64(parts[i].Number),
		}
	}

	params := &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(h.Bucket),
		Key:             aws.String(h.path(upload.SessionID)),
		UploadId:        aws.String(upload.ID),
		MultipartUpload: &s3.CompletedMultipartUpload{Parts: completedParts},
	}
	_, err := h.client.CompleteMultipartUploadWithContext(ctx, params)
	if err != nil {
		return awsutils.ConvertS3Error(err)
	}
	return nil
}

// ListParts lists upload parts
func (h *Handler) ListParts(ctx context.Context, upload events.StreamUpload) ([]events.StreamPart, error) {
	var parts []events.StreamPart
	var partNumberMarker *int64
	for i := 0; i < defaults.MaxIterationLimit; i++ {
		re, err := h.client.ListPartsWithContext(ctx, &s3.ListPartsInput{
			Bucket:           aws.String(h.Bucket),
			Key:              aws.String(h.path(upload.SessionID)),
			UploadId:         aws.String(upload.ID),
			PartNumberMarker: partNumberMarker,
		})
		if err != nil {
			return nil, awsutils.ConvertS3Error(err)
		}
		for _, part := range re.Parts {
			parts = append(parts, events.StreamPart{
				Number: *part.PartNumber,
				ETag:   *part.ETag,
			})
		}
		if !aws.BoolValue(re.IsTruncated) {
			break
		}
		partNumberMarker = re.PartNumberMarker
	}
	// Parts must be sorted in PartNumber order.
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].Number < parts[j].Number
	})
	return parts, nil
}

// ListUploads lists uploads that have been initiated but not completed
func (h *Handler) ListUploads(ctx context.Context) ([]events.StreamUpload, error) {
	var prefix *string
	if h.Path != "" {
		trimmed := strings.TrimPrefix(h.Path, "/")
		prefix = &trimmed
	}
	var uploads []events.StreamUpload
	var keyMarker *string
	var uploadIDMarker *string
	for i := 0; i < defaults.MaxIterationLimit; i++ {
		input := &s3.ListMultipartUploadsInput{
			Bucket:         aws.String(h.Bucket),
			Prefix:         prefix,
			KeyMarker:      keyMarker,
			UploadIdMarker: uploadIDMarker,
		}
		re, err := h.client.ListMultipartUploadsWithContext(ctx, input)
		if err != nil {
			return nil, awsutils.ConvertS3Error(err)
		}
		for _, upload := range re.Uploads {
			uploads = append(uploads, events.StreamUpload{
				ID:        *upload.UploadId,
				SessionID: h.fromPath(*upload.Key),
				Initiated: *upload.Initiated,
			})
		}
		if !aws.BoolValue(re.IsTruncated) {
			break
		}
		keyMarker = re.KeyMarker
		uploadIDMarker = re.UploadIdMarker
	}

	sort.Slice(uploads, func(i, j int) bool {
		return uploads[i].Initiated.Before(uploads[j].Initiated)
	})

	return uploads, nil
}

// GetUploadMetadata gets the metadata for session upload
func (h *Handler) GetUploadMetadata(sessionID session.ID) events.UploadMetadata {
	return events.UploadMetadata{
		URL:       fmt.Sprintf("%v://%v/%v", teleport.SchemeS3, h.Bucket, sessionID),
		SessionID: sessionID,
	}
}

// ReserveUploadPart reserves an upload part.
func (h *Handler) ReserveUploadPart(ctx context.Context, upload events.StreamUpload, partNumber int64) error {
	return nil
}
