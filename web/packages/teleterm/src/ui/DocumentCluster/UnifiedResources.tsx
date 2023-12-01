/**
 * Copyright 2023 Gravitational, Inc
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import React, { useCallback, useEffect } from 'react';

import {
  UnifiedResources as SharedUnifiedResources,
  useUnifiedResourcesFetch,
  UnifiedResourcesQueryParams,
  SharedUnifiedResource,
  UnifiedResourcesPinning,
} from 'shared/components/UnifiedResources';
import {
  DbProtocol,
  formatDatabaseInfo,
  DbType,
} from 'shared/services/databases';

import { Flex, ButtonPrimary, Text } from 'design';

import * as icons from 'design/Icon';
import Image from 'design/Image';
import stack from 'design/assets/resources/stack.png';

import {
  DefaultTab,
  ViewMode,
} from 'shared/services/unifiedResourcePreferences';

import {
  useAsync,
  mapAttempt,
  Attempt,
  makeSuccessAttempt,
} from 'shared/hooks/useAsync';

import {
  UnifiedResourceResponse,
  UserPreferences,
} from 'teleterm/services/tshd/types';
import { useAppContext } from 'teleterm/ui/appContextProvider';
import * as uri from 'teleterm/ui/uri';
import { useWorkspaceContext } from 'teleterm/ui/Documents';
import { useWorkspaceLoggedInUser } from 'teleterm/ui/hooks/useLoggedInUser';
import { useConnectMyComputerContext } from 'teleterm/ui/ConnectMyComputer';

import { retryWithRelogin } from 'teleterm/ui/utils';
import {
  DocumentClusterQueryParams,
  DocumentCluster,
  DocumentClusterResourceKind,
} from 'teleterm/ui/services/workspacesService';

import {
  ConnectServerActionButton,
  ConnectKubeActionButton,
  ConnectDatabaseActionButton,
} from './actionButtons';
import { useResourcesContext } from './resourcesContext';

export function UnifiedResources(props: {
  clusterUri: uri.ClusterUri;
  docUri: uri.DocumentUri;
  queryParams: DocumentClusterQueryParams;
}) {
  const appContext = useAppContext();
  const [
    userPreferencesAttempt,
    runUserPreferencesAttempt,
    setUserPreferences,
  ] = useAsync(
    useCallback(async () => {
      const preferences = await appContext.tshd.getUserPreferences({
        clusterUri: props.clusterUri,
      });
      const { unifiedResourcePreferences, clusterPreferences } = preferences;

      // TODO(gzdunek): Remove the fallback in v16.
      // Support for UnifiedTabPreference has been added in 14.1 and for
      // UnifiedViewModePreference in 14.1.5.
      // We have to support these values being undefined/unset in Connect v15.
      const unifiedResourcePreferencesWithFallback = {
        defaultTab: unifiedResourcePreferences
          ? unifiedResourcePreferences.defaultTab
          : DefaultTab.DEFAULT_TAB_ALL,
        viewMode:
          unifiedResourcePreferences &&
          unifiedResourcePreferences.viewMode !== ViewMode.VIEW_MODE_UNSPECIFIED
            ? unifiedResourcePreferences.viewMode
            : ViewMode.VIEW_MODE_CARD,
      };
      return {
        clusterPreferences,
        unifiedResourcePreferences: unifiedResourcePreferencesWithFallback,
      };
    }, [appContext.tshd, props.clusterUri])
  );

  useEffect(() => {
    if (userPreferencesAttempt.status === '') {
      runUserPreferencesAttempt();
    }
  }, [runUserPreferencesAttempt, userPreferencesAttempt.status]);

  async function updateUserPreferences(
    newPreferences: Partial<UserPreferences>
  ) {
    setUserPreferences(prevState => {
      return makeSuccessAttempt({ ...prevState.data, ...newPreferences });
    });
    // TODO(gzdunek): handle errors
    await appContext.tshd.updateUserPreferences({
      clusterUri: props.clusterUri,
      userPreferences: newPreferences,
    });
  }

  const mergedParams: UnifiedResourcesQueryParams = {
    kinds: props.queryParams.resourceKinds,
    sort: props.queryParams.sort,
    pinnedOnly:
      userPreferencesAttempt.status === 'success' &&
      userPreferencesAttempt.data.unifiedResourcePreferences.defaultTab ===
        DefaultTab.DEFAULT_TAB_PINNED,
    search: props.queryParams.advancedSearchEnabled
      ? ''
      : props.queryParams.search,
    query: props.queryParams.advancedSearchEnabled
      ? props.queryParams.search
      : '',
  };

  return (
    <Resources
      queryParams={mergedParams}
      docUri={props.docUri}
      clusterUri={props.clusterUri}
      userPreferencesAttempt={userPreferencesAttempt}
      updateUserPreferences={updateUserPreferences}
      // Reset the component state when query params object change.
      // JSON.stringify on the same object will always produce the same string.
      key={JSON.stringify(mergedParams)}
    />
  );
}

function Resources(props: {
  clusterUri: uri.ClusterUri;
  docUri: uri.DocumentUri;
  queryParams: UnifiedResourcesQueryParams;
  userPreferencesAttempt: Attempt<UserPreferences>;
  updateUserPreferences(u: UserPreferences): Promise<void>;
}) {
  const appContext = useAppContext();
  const { onResourcesRefreshRequest } = useResourcesContext();

  const { documentsService, rootClusterUri } = useWorkspaceContext();
  const loggedInUser = useWorkspaceLoggedInUser();
  const { canUse: hasPermissionsForConnectMyComputer, agentCompatibility } =
    useConnectMyComputerContext();

  const isRootCluster = props.clusterUri === rootClusterUri;
  const canAddResources = isRootCluster && loggedInUser?.acl?.tokens.create;

  const canUseConnectMyComputer =
    isRootCluster &&
    hasPermissionsForConnectMyComputer &&
    agentCompatibility === 'compatible';

  const { fetch, resources, attempt, clear } = useUnifiedResourcesFetch({
    fetchFunc: useCallback(
      async (paginationParams, signal) => {
        const response = await retryWithRelogin(
          appContext,
          props.clusterUri,
          () =>
            appContext.resourcesService.listUnifiedResources(
              {
                clusterUri: props.clusterUri,
                searchAsRoles: false,
                sortBy: {
                  isDesc: props.queryParams.sort.dir === 'DESC',
                  field: props.queryParams.sort.fieldName,
                },
                search: props.queryParams.search,
                kindsList: props.queryParams.kinds,
                query: props.queryParams.query,
                pinnedOnly: props.queryParams.pinnedOnly,
                startKey: paginationParams.startKey,
                limit: paginationParams.limit,
              },
              signal
            )
        );

        return {
          startKey: response.nextKey,
          agents: response.resources,
          totalCount: response.resources.length,
        };
      },
      [
        appContext,
        props.queryParams.kinds,
        props.queryParams.pinnedOnly,
        props.queryParams.query,
        props.queryParams.search,
        props.queryParams.sort.dir,
        props.queryParams.sort.fieldName,
        props.clusterUri,
      ]
    ),
  });

  useEffect(() => {
    const { cleanup } = onResourcesRefreshRequest(() => {
      clear();
      fetch({ force: true });
    });
    return cleanup;
  }, [onResourcesRefreshRequest, fetch, clear]);

  function onParamsChange(newParams: UnifiedResourcesQueryParams): void {
    const documentService =
      appContext.workspacesService.getWorkspaceDocumentService(
        uri.routing.ensureRootClusterUri(props.clusterUri)
      );
    documentService.update(props.docUri, (draft: DocumentCluster) => {
      const { queryParams } = draft;
      queryParams.sort = newParams.sort;
      queryParams.resourceKinds =
        newParams.kinds as DocumentClusterResourceKind[];
      queryParams.search = newParams.search || newParams.query;
    });
  }

  function getPinning(): UnifiedResourcesPinning {
    // optimistically assume that pinning is supported
    const isPinningSupported =
      props.userPreferencesAttempt.status === '' ||
      props.userPreferencesAttempt.status === 'processing' ||
      (
        props.userPreferencesAttempt.status === 'success' &&
        props.userPreferencesAttempt.data.clusterPreferences?.pinnedResources
      )?.resourceIdsList;
    return isPinningSupported
      ? {
          kind: 'supported',
          getClusterPinnedResources: fetchPinnedResources,
          updateClusterPinnedResources: updatePinnedResources,
        }
      : { kind: 'not-supported' };
  }

  const fetchPinnedResources = useCallback(async () => {
    if (props.userPreferencesAttempt.status === 'success') {
      return props.userPreferencesAttempt.data.clusterPreferences
        .pinnedResources.resourceIdsList;
    }
    return [];
  }, [props.userPreferencesAttempt]);
  const updatePinnedResources = (pinnedIds: string[]) =>
    props.updateUserPreferences({
      clusterPreferences: { pinnedResources: { resourceIdsList: pinnedIds } },
    });

  return (
    <SharedUnifiedResources
      params={props.queryParams}
      setParams={onParamsChange}
      unifiedResourcePreferencesAttempt={mapAttempt(
        props.userPreferencesAttempt,
        attemptData => attemptData.unifiedResourcePreferences
      )}
      updateUnifiedResourcesPreferences={unifiedResourcePreferences =>
        props.updateUserPreferences({ unifiedResourcePreferences })
      }
      onLabelClick={() => alert('Not implemented')}
      pinning={getPinning()}
      resources={resources.map(mapToSharedResource)}
      resourcesFetchAttempt={attempt}
      fetchResources={fetch}
      availableKinds={[
        {
          kind: 'node',
          disabled: false,
        },
        {
          kind: 'db',
          disabled: false,
        },
        {
          kind: 'kube_cluster',
          disabled: false,
        },
      ]}
      NoResources={
        <NoResources
          canCreate={canAddResources}
          canUseConnectMyComputer={canUseConnectMyComputer}
          onConnectMyComputerCtaClick={() => {
            documentsService.openConnectMyComputerDocument({ rootClusterUri });
          }}
        />
      }
    />
  );
}

const mapToSharedResource = (
  resource: UnifiedResourceResponse
): SharedUnifiedResource => {
  switch (resource.kind) {
    case 'server': {
      const { resource: server } = resource;
      return {
        resource: {
          kind: 'node' as const,
          labels: server.labelsList,
          id: server.name,
          hostname: server.hostname,
          addr: server.addr,
          tunnel: server.tunnel,
          subKind: server.subKind,
        },
        ui: {
          ActionButton: <ConnectServerActionButton server={server} />,
        },
      };
    }
    case 'database': {
      const { resource: database } = resource;
      return {
        resource: {
          kind: 'db' as const,
          labels: database.labelsList,
          description: database.desc,
          name: database.name,
          type: formatDatabaseInfo(
            database.type as DbType,
            database.protocol as DbProtocol
          ).title,
          protocol: database.protocol as DbProtocol,
        },
        ui: {
          ActionButton: <ConnectDatabaseActionButton database={database} />,
        },
      };
    }
    case 'kube': {
      const { resource: kube } = resource;

      return {
        resource: {
          kind: 'kube_cluster' as const,
          labels: kube.labelsList,
          name: kube.name,
        },
        ui: {
          ActionButton: <ConnectKubeActionButton kube={kube} />,
        },
      };
    }
  }
};

function NoResources(props: {
  canCreate: boolean;
  canUseConnectMyComputer: boolean;
  onConnectMyComputerCtaClick(): void;
}) {
  let $content: React.ReactElement;
  if (!props.canCreate) {
    $content = (
      <>
        <Text typography="h3" mb="2" fontWeight={600}>
          No Resources Found
        </Text>
        <Text>
          Either there are no resources in the cluster, or your roles don't
          grant you access.
        </Text>
      </>
    );
  } else {
    $content = (
      <>
        <Image src={stack} ml="auto" mr="auto" mb={4} height="100px" />
        <Text typography="h3" mb={2} fontWeight={600}>
          Add your first resource to Teleport
        </Text>
        <Text color="text.slightlyMuted">
          {props.canUseConnectMyComputer
            ? 'You can add it in the Teleport Web UI or by connecting your computer to the cluster.'
            : 'Connect SSH servers, Kubernetes clusters, Databases and more from Teleport Web UI.'}
        </Text>
        {props.canUseConnectMyComputer && (
          <ButtonPrimary
            type="button"
            mt={3}
            gap={2}
            onClick={props.onConnectMyComputerCtaClick}
          >
            <icons.Laptop size={'medium'} />
            Connect My Computer
          </ButtonPrimary>
        )}
      </>
    );
  }

  return (
    <Flex
      maxWidth={600}
      p={8}
      pt={5}
      width="100%"
      mx="auto"
      flexDirection="column"
      alignItems="center"
      justifyContent="center"
    >
      {$content}
    </Flex>
  );
}
