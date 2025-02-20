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

import React from 'react';

import {
  ManagementSection,
  NavigationCategory,
} from 'teleport/Navigation/categories';

export type NavGroup = 'team' | 'activity' | 'clusters' | 'accessrequests';

export interface Context {
  init(): Promise<void>;
  getFeatureFlags(): FeatureFlags;
}

export interface TeleportFeatureNavigationItem {
  title: NavTitle;
  icon: React.ReactNode;
  exact?: boolean;
  getLink?(clusterId: string): string;
  isExternalLink?: boolean;
}

export enum NavTitle {
  // Resources
  Servers = 'Servers',
  Applications = 'Applications',
  Kubernetes = 'Kubernetes',
  Databases = 'Databases',
  Desktops = 'Desktops',
  AccessRequests = 'Access Requests',
  ActiveSessions = 'Active Sessions',
  Resources = 'Resources',

  // Access Management
  Users = 'Users',
  Roles = 'User Roles',
  AuthConnectors = 'Auth Connectors',
  Integrations = 'Integrations',
  EnrollNewResource = 'Enroll New Resource',
  EnrollNewIntegration = 'Enroll New Integration',

  // Identity Governance & Security
  AccessLists = 'Access Lists',
  SessionAndIdentityLocks = 'Session & Identity Locks',
  TrustedDevices = 'Trusted Devices',
  AccessMonitoring = 'Access Monitoring',

  // Resources Requests
  NewRequest = 'New Request',
  ReviewRequests = 'Review Requests',
  AccessGraph = 'Access Graph',

  // Activity
  SessionRecordings = 'Session Recordings',
  AuditLog = 'Audit Log',

  // Billing
  BillingSummary = 'Summary',
  PaymentsAndInvoices = 'Payments and Invoices',
  InvoiceSettings = 'Invoice Settings',

  // Clusters
  ManageClusters = 'Manage Clusters',
  TrustedClusters = 'Trusted Clusters',

  // Account
  AccountSettings = 'Account Settings',
  HelpAndSupport = 'Help & Support',

  Support = 'Support',
  Downloads = 'Downloads',
}

export interface TeleportFeatureRoute {
  title: string;
  path: string;
  exact?: boolean;
  component: React.FunctionComponent;
}

export interface TeleportFeature {
  parent?: new () => TeleportFeature | null;
  category?: NavigationCategory;
  section?: ManagementSection;
  hasAccess(flags: FeatureFlags): boolean;
  hideFromNavigation?: boolean;
  // route defines react router Route fields.
  // This field can be left undefined to indicate
  // this feature is a parent to children features
  // eg: FeatureAccessRequests is parent to sub features
  // FeatureNewAccessRequest and FeatureReviewAccessRequests.
  // These childrens will be responsible for routing.
  route?: TeleportFeatureRoute;
  navigationItem?: TeleportFeatureNavigationItem;
  topMenuItem?: TeleportFeatureNavigationItem;
  // alternative items to display when the user has permissions (RBAC)
  // but the cluster lacks the feature:
  isLocked?(lockedFeatures: LockedFeatures): boolean;
  lockedNavigationItem?: TeleportFeatureNavigationItem;
  lockedRoute?: TeleportFeatureRoute;
}

export type StickyCluster = {
  clusterId: string;
  hasClusterUrl: boolean;
  isLeafCluster: boolean;
};

export type Label = {
  name: string;
  value: string;
};

// TODO: create a better abscraction for a filter, right now it's just a label
export type Filter = {
  value: string;
  name: string;
  kind: 'label';
};

export interface FeatureFlags {
  audit: boolean;
  recordings: boolean;
  authConnector: boolean;
  roles: boolean;
  trustedClusters: boolean;
  users: boolean;
  applications: boolean;
  kubernetes: boolean;
  billing: boolean;
  databases: boolean;
  desktops: boolean;
  nodes: boolean;
  activeSessions: boolean;
  accessRequests: boolean;
  newAccessRequest: boolean;
  downloadCenter: boolean;
  discover: boolean;
  plugins: boolean;
  integrations: boolean;
  enrollIntegrationsOrPlugins: boolean;
  enrollIntegrations: boolean;
  deviceTrust: boolean;
  locks: boolean;
  newLocks: boolean;
  assist: boolean;
  accessMonitoring: boolean;
  // Whether or not the management section should be available.
  managementSection: boolean;
  accessGraph: boolean;
  externalAuditStorage: boolean;
}

// LockedFeatures are used for determining which features are disabled in the user's cluster.
export type LockedFeatures = {
  authConnectors: boolean;
  activeSessions: boolean;
  accessRequests: boolean;
  premiumSupport: boolean;
  trustedDevices: boolean;
  externalCloudAudit: boolean;
};

// RecommendFeature is used for recommending features if its usage status is zero.
export type RecommendFeature = {
  TrustedDevices: RecommendationStatus;
};

export enum RecommendationStatus {
  Notify = 'NOTIFY',
  Done = 'DONE',
}
