/**
 Copyright 2023 Gravitational, Inc.

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

import styled from 'styled-components';
import React from 'react';

import * as Icons from 'design/Icon';
import { Flex, Text } from 'design';
import { TeleportGearIcon } from 'design/SVGIcon';

import { MenuIcon } from 'shared/components/MenuAction';

function colorForLatency(l: number): string {
  if (l > 400) {
    return 'dataVisualisation.tertiary.abbey';
  }

  if (l > 150) {
    return 'dataVisualisation.tertiary.sunflower';
  }

  return 'dataVisualisation.tertiary.caribbean';
}

export function LatencyDiagnostic({ latency }: LatencyDiagnosticProps) {
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

export type LatencyDiagnosticProps = {
  latency: {
    client: number;
    server: number;
    total: number;
  };
};
