/**
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

import Logger from 'shared/libs/logger';

import { context, defaultTextMapSetter, trace } from '@opentelemetry/api';
import { W3CTraceContextPropagator } from '@opentelemetry/core';

import webSession from 'teleport/services/websession';
import history from 'teleport/services/history';
import cfg, { UrlResourcesParams, UrlSshParams } from 'teleport/config';
import { getAccessToken, getHostName } from 'teleport/services/api';
import Tty from 'teleport/lib/term/tty';
import TtyAddressResolver from 'teleport/lib/term/ttyAddressResolver';
import serviceSession, {
  Session,
  ParticipantList,
  ParticipantMode,
} from 'teleport/services/session';
import ServiceNodes from 'teleport/services/nodes';
import serviceClusters from 'teleport/services/clusters';
import { StoreUserContext } from 'teleport/stores';
import usersService from 'teleport/services/user';

import { StoreParties, StoreDocs, DocumentSsh, Document } from './stores';

const logger = Logger.create('teleport/console');

const tracer = trace.getTracer('console-context');

/**
 * Console Context is used by components to access shared state and also to communicate
 * with other services.
 */
export default class ConsoleContext {
  storeDocs = new StoreDocs();
  storeParties = new StoreParties();
  nodesService = new ServiceNodes();
  storeUser = new StoreUserContext();

  constructor() {
    // always initialize the console with 1 document
    this.storeDocs.add({
      kind: 'blank',
      url: cfg.getConsoleRoute(cfg.proxyCluster),
      clusterId: cfg.proxyCluster,
      created: new Date(),
    });
  }

  async initStoreUser() {
    const user = await usersService.fetchUserContext();
    this.storeUser.setState(user);
  }

  getStoreUser() {
    return this.storeUser.state;
  }

  getActiveDocId(url: string) {
    const doc = this.storeDocs.findByUrl(url);
    return doc ? doc.id : -1;
  }

  removeDocument(id: number) {
    const nextId = this.storeDocs.getNext(id);
    const items = this.storeDocs.filter(id);
    this.storeDocs.setState({ items });
    return this.storeDocs.find(nextId);
  }

  updateSshDocument(id: number, partial: Partial<DocumentSsh>) {
    this.storeDocs.update(id, partial);
  }

  addNodeDocument(clusterId = cfg.proxyCluster) {
    return this.storeDocs.add({
      clusterId,
      title: `New session`,
      kind: 'nodes',
      url: cfg.getConsoleNodesRoute(clusterId),
      created: new Date(),
    });
  }

  addSshDocument({ login, serverId, sid, clusterId, mode }: UrlSshParams) {
    const title = login && serverId ? `${login}@${serverId}` : sid;
    const url = this.getSshDocumentUrl({
      clusterId,
      login,
      serverId,
      sid,
      mode,
    });

    return this.storeDocs.add({
      kind: 'terminal',
      status: 'disconnected',
      clusterId,
      title,
      serverId,
      login,
      sid,
      url,
      mode,
      created: new Date(),
    });
  }

  getDocuments() {
    return this.storeDocs.state.items;
  }

  getNodeDocumentUrl(clusterId: string) {
    return cfg.getConsoleNodesRoute(clusterId);
  }

  getSshDocumentUrl(sshParams: UrlSshParams) {
    return sshParams.sid
      ? cfg.getSshSessionRoute(sshParams)
      : cfg.getSshConnectRoute(sshParams);
  }

  refreshParties() {
    return tracer.startActiveSpan('refreshParties', span => {
      // Finds unique clusterIds from all active ssh sessions
      // and creates a separate API call per each.
      // After receiving the data, it updates the stores only once.
      const clusters = this.storeDocs
        .getSshDocuments()
        .filter(doc => doc.status === 'connected')
        .map(doc => doc.clusterId);

      const unique = [...new Set(clusters)];
      const requests = unique.map(clusterId =>
        // Fetch parties for a given cluster and in case of an error
        // return an empty object.
        serviceSession.fetchParticipants({ clusterId }).catch(err => {
          logger.error('failed to refresh participants', err);
          const emptyResults: ParticipantList = {};
          return emptyResults;
        })
      );

      return Promise.all(requests).then(results => {
        let parties: ParticipantList = {};
        for (let i = 0; i < results.length; i++) {
          parties = {
            ...results[i],
          };
        }

        this.storeParties.setParties(parties);
        span.end();
      });
    });
  }

  fetchNodes(clusterId: string, params?: UrlResourcesParams) {
    return this.nodesService.fetchNodes(clusterId, params);
  }

  fetchClusters() {
    return serviceClusters.fetchClusters();
  }

  logout() {
    webSession.logout();
  }

  createTty(session: Session, mode?: ParticipantMode): Tty {
    const { login, sid, serverId, clusterId } = session;

    const propagator = new W3CTraceContextPropagator();
    let carrier = {};

    const ctx = context.active();

    propagator.inject(ctx, carrier, defaultTextMapSetter);

    const ttyUrl = cfg.api.ttyWsAddr
      .replace(':fqdn', getHostName())
      .replace(':token', getAccessToken())
      .replace(':clusterId', clusterId)
      .replace(':traceparent', carrier['traceparent']);

    const addressResolver = new TtyAddressResolver({
      ttyUrl,
      ttyParams: {
        login,
        sid,
        server_id: serverId,
        mode,
      },
    });

    return new Tty(addressResolver);
  }

  gotoNodeTab(clusterId: string) {
    const url = this.getNodeDocumentUrl(clusterId);
    this.gotoTab({ url });
  }

  gotoTab({ url }: { url: string }, replace = true) {
    if (replace) {
      history.replace(url);
    } else {
      history.push(url);
    }
  }

  closeTab(doc: Document) {
    const next = this.removeDocument(doc.id);
    this.gotoTab(next);
  }
}
