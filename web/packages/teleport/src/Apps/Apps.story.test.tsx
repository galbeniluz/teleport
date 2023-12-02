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
import { render, screen } from 'design/utils/testing';

import { KeysEnum } from 'teleport/services/storageService';

import { Loaded, Failed, Empty, EmptyReadOnly } from './Apps.story';

jest.mock('teleport/useStickyClusterId', () =>
  jest.fn(() => ({ clusterId: 'im-a-cluster', isLeafCluster: false }))
);

// TODO (avatus) DELETE IN 15.0
// this is to allow the tests to actually render
// the correct tables
beforeAll(() => {
  localStorage.setItem(KeysEnum.UNIFIED_RESOURCES_DISABLED, 'true');
});

afterAll(() => {
  localStorage.removeItem(KeysEnum.UNIFIED_RESOURCES_DISABLED);
});

test('loaded state', async () => {
  const { container } = render(<Loaded />);
  await screen.findAllByText(/Applications/i);

  expect(container).toMatchSnapshot();
});

test('failed state', async () => {
  const { container } = render(<Failed />);
  await screen.findAllByText(/some error message/i);

  expect(container).toMatchSnapshot();
});

test('empty state for enterprise, can create', () => {
  const { container } = render(<Empty />);
  expect(container).toMatchSnapshot();
});

test('readonly empty state', () => {
  const { container } = render(<EmptyReadOnly />);
  expect(container).toMatchSnapshot();
});
