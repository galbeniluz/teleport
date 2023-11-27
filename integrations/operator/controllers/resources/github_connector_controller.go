/*
Copyright 2022 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"context"

	"github.com/gravitational/trace"
	kclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gravitational/teleport/api/client"
	"github.com/gravitational/teleport/api/types"
	resourcesv3 "github.com/gravitational/teleport/integrations/operator/apis/resources/v3"
)

// githubConnectorClient implements TeleportResourceClient and offers CRUD methods needed to reconcile github_connectors
type githubConnectorClient struct {
	teleportClient *client.Client
}

// Get gets the Teleport github_connector of a given name
func (r githubConnectorClient) Get(ctx context.Context, name string) (types.GithubConnector, error) {
	github, err := r.teleportClient.GetGithubConnector(ctx, name, false /* with secrets*/)
	return github, trace.Wrap(err)
}

// Create creates a Teleport github_connector
func (r githubConnectorClient) Create(ctx context.Context, github types.GithubConnector) error {
	return trace.Wrap(r.teleportClient.UpsertGithubConnector(ctx, github))
}

// Update updates a Teleport github_connector
func (r githubConnectorClient) Update(ctx context.Context, github types.GithubConnector) error {
	return trace.Wrap(r.teleportClient.UpsertGithubConnector(ctx, github))
}

// Delete deletes a Teleport github_connector
func (r githubConnectorClient) Delete(ctx context.Context, name string) error {
	return trace.Wrap(r.teleportClient.DeleteGithubConnector(ctx, name))
}

// NewGithubConnectorReconciler instantiates a new Kubernetes controller reconciling github_connector resources
func NewGithubConnectorReconciler(client kclient.Client, tClient *client.Client) *TeleportResourceReconciler[types.GithubConnector, *resourcesv3.TeleportGithubConnector] {
	githubClient := &githubConnectorClient{
		teleportClient: tClient,
	}

	resourceReconciler := NewTeleportResourceReconciler[types.GithubConnector, *resourcesv3.TeleportGithubConnector](
		client,
		githubClient,
	)

	return resourceReconciler
}
