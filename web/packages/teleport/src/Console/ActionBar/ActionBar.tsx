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

import styled from 'styled-components';
import { NavLink } from 'react-router-dom';
import { MenuIcon, MenuItem, MenuItemIcon } from 'shared/components/MenuAction';
import * as Icons from 'design/Icon';
import { Flex, ButtonPrimary, Text } from 'design';
import { TeleportGearIcon } from 'design/SVGIcon';

import cfg from 'teleport/config';

export default function ActionBar(props: Props) {
  return (
    <Flex alignItems="center">
      <LatencyDiagnostic latency={props.latency} />
      <MenuIcon
        buttonIconProps={{ mr: 2, ml: 2, size: 0, style: { fontSize: '16px' } }}
        menuProps={menuProps}
      >
        <MenuItem as={NavLink} to={cfg.routes.root}>
          <MenuItemIcon as={Icons.Home} mr="2" size="medium" />
          Home
        </MenuItem>
        <MenuItem>
          <ButtonPrimary my={3} block onClick={props.onLogout}>
            Sign Out
          </ButtonPrimary>
        </MenuItem>
      </MenuIcon>
    </Flex>
  );
}

function colorForLatency(l: number): string {
  if (l > 400) {
    return 'dataVisualisation.tertiary.abbey';
  }

  if (l > 150) {
    return 'dataVisualisation.tertiary.sunflower';
  }

  return 'dataVisualisation.tertiary.caribbean';
}

function LatencyDiagnostic({ latency }: LatencyDiagnosticProps) {
  const totalColor = colorForLatency(latency.total);
  const clientColor = colorForLatency(latency.client);
  const serverColor = colorForLatency(latency.server);

  return (
    <MenuIcon Icon={Icons.Wifi} buttonIconProps={{ color: totalColor }}>
      <Container>
        <Flex gap={5} flexDirection="column">
          <Text textAlign="left" typography="h3">
            Network Connection
          </Text>

          <Flex flexDirection="row" alignItems="center">
            <Flex mr={2} gap={1} flexDirection="column" alignItems="center">
              <Icons.User />
              <Text>You</Text>
            </Flex>

            <Flex mr={2} gap={1} flexDirection="column" alignItems="center">
              <Flex mr={2} gap={1} flexDirection="row" alignItems="center">
                <Icons.ChevronLeft size="medium" />
                <Line />
                <Icons.ChevronRight size="medium" />
              </Flex>
              <Text color={clientColor}>{latency.client}ms</Text>
            </Flex>

            <Flex mr={2} gap={1} flexDirection="column" alignItems="center">
              <TeleportGearIcon size={24}></TeleportGearIcon>
              <Text>Teleport</Text>
            </Flex>

            <Flex mr={2} gap={1} flexDirection="column" alignItems="center">
              <Flex mr={2} gap={1} flexDirection="row" alignItems="center">
                <Icons.ChevronLeft size="medium" />
                <Line />
                <Icons.ChevronRight size="medium" />
              </Flex>
              <Text color={serverColor}>{latency.server}ms</Text>
            </Flex>

            <Flex mr={2} gap={1} flexDirection="column" alignItems="center">
              <Icons.Server />
              <Text>Server</Text>
            </Flex>
          </Flex>

          <Flex flexDirection="column" alignItems="center">
            <Text bold fontSize={2} textAlign="center" color={totalColor}>
              Total Latency: {latency.total}ms
            </Text>
          </Flex>
        </Flex>
      </Container>
    </MenuIcon>
  );
}

const Container = styled.div`
  background: ${props => props.theme.colors.levels.surface};
  padding: ${props => props.theme.space[4]}px;
  width: 370px;
  height: 164px;
`;

const Line = styled.div`
  border: 1px dashed;
  width: 55px;
`;

type LatencyDiagnosticProps = {
  latency: {
    client: number;
    server: number;
    total: number;
  };
};

type Props = {
  onLogout: VoidFunction;
  latency: {
    client: number;
    server: number;
    total: number;
  };
};

const menuListCss = () => `
  width: 250px;
`;

const menuProps = {
  menuListCss,
} as const;
