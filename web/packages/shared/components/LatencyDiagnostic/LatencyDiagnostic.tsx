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

export const warnThreshold = 150;
export const errorThreshold = 400;

export enum latencyColor {
  ok = 'dataVisualisation.tertiary.caribbean',
  warn = 'dataVisualisation.tertiary.abbey',
  error = 'dataVisualisation.tertiary.sunflower',
}

// latencyColors determines the color to use for each leg of the connection
// and the total measurement. The total color is derived from each leg of
// the connection instead of the combined latency to prevent two latencies
// in the same threshold from causing the total to jump into the next threshold.
// For example, this prevents showing a warning if both legs of the connection are ok,
// but their sum would result in exceeding the warning threshold.
export function latencyColors(
  client: number,
  server: number
): { client: latencyColor; server: latencyColor; total: latencyColor } {
  function colorForLatency(l: number): latencyColor {
    if (l >= errorThreshold) {
      return latencyColor.error;
    }

    if (l >= warnThreshold) {
      return latencyColor.warn;
    }

    return latencyColor.ok;
  }

  const clientColor = colorForLatency(client);
  const serverColor = colorForLatency(server);

  // any + red = red
  if (client >= errorThreshold || server >= errorThreshold) {
    return {
      client: clientColor,
      server: serverColor,
      total: latencyColor.error,
    };
  }

  // yellow + yellow = yellow
  if (client >= warnThreshold && server >= warnThreshold) {
    return {
      client: clientColor,
      server: serverColor,
      total: latencyColor.warn,
    };
  }

  // green + yellow = yellow
  if (
    (client >= warnThreshold || server >= warnThreshold) &&
    (client < warnThreshold || server < warnThreshold)
  ) {
    return {
      client: clientColor,
      server: serverColor,
      total: latencyColor.warn,
    };
  }

  // green + green = green
  return { client: clientColor, server: serverColor, total: latencyColor.ok };
}

export function LatencyDiagnostic({ latency }: LatencyDiagnosticProps) {
  const colors = latencyColors(latency.client, latency.server);

  return (
    <MenuIcon Icon={Icons.Wifi} buttonIconProps={{ color: colors.total }}>
      <Container>
        <Flex gap={5} flexDirection="column">
          <Text textAlign="left" typography="h3">
            Network Connection
          </Text>

          <Flex alignItems="center">
            <Flex
              gap={1}
              width="24px"
              flexDirection="column"
              alignItems="flex-start"
              css={`
                flex-grow: 0;
              `}
            >
              <Icons.User />
              <Text>You</Text>
            </Flex>

            <Flex
              gap={1}
              flexDirection="column"
              alignItems="center"
              css={`
                flex-grow: 1;
                position: relative;
              `}
            >
              <Flex
                gap={1}
                flexDirection="row"
                alignItems="center"
                width="100%"
                css={`
                  padding: 0 16px;
                `}
              >
                <Icons.ChevronLeft
                  size="small"
                  color="text.muted"
                  css={`
                    left: 8px;
                    position: absolute;
                  `}
                />
                <Line />
                <Icons.ChevronRight
                  size="small"
                  color="text.muted"
                  css={`
                    right: 8px;
                    position: absolute;
                  `}
                />
              </Flex>
              <Text color={colors.client}>{latency.client}ms</Text>
            </Flex>

            <Flex
              gap={1}
              width="24px"
              flexDirection="column"
              alignItems="center"
              css={`
                flex-grow: 0;
              `}
            >
              <TeleportGearIcon size={24}></TeleportGearIcon>
              <Text>Teleport</Text>
            </Flex>

            <Flex
              gap={1}
              flexDirection="column"
              alignItems="center"
              css={`
                flex-grow: 1;
                position: relative;
              `}
            >
              <Flex
                gap={1}
                flexDirection="row"
                alignItems="center"
                width="100%"
                css={`
                  padding: 0 16px;
                `}
              >
                <Icons.ChevronLeft
                  size="small"
                  color="text.muted"
                  css={`
                    left: 8px;
                    position: absolute;
                  `}
                />
                <Line />
                <Icons.ChevronRight
                  size="small"
                  color="text.muted"
                  css={`
                    right: 8px;
                    position: absolute;
                  `}
                />
              </Flex>
              <Text color={colors.server}>{latency.server}ms</Text>
            </Flex>

            <Flex
              gap={1}
              width="24px"
              flexDirection="column"
              alignItems="flex-end"
              css={`
                flex-grow: 0;
              `}
            >
              <Icons.Server />
              <Text>Server</Text>
            </Flex>
          </Flex>

          <Flex flexDirection="column" alignItems="center">
            <Flex gap={1} flexDirection="row" alignItems="center">
              {colors.total === latencyColor.error && (
                <Icons.WarningCircle size={20} color={colors.total} />
              )}

              {colors.total === latencyColor.warn && (
                <Icons.Warning size={20} color={colors.total} />
              )}

              {colors.total === latencyColor.ok && (
                <Icons.CircleCheck size={20} color={colors.total} />
              )}

              <Text bold fontSize={2} textAlign="center" color={colors.total}>
                Total Latency: {latency.total}ms
              </Text>
            </Flex>
          </Flex>
        </Flex>
      </Container>
    </MenuIcon>
  );
}

const Container = styled.div`
  background: ${props => props.theme.colors.levels.elevated};
  padding: ${props => props.theme.space[4]}px;
  width: 370px;
  height: 164px;
`;

const Line = styled.div`
  color: ${props => props.theme.colors.text.muted};
  border: 0.5px dashed;
  min-width: 60px;
  width: 100%;
`;

export type LatencyDiagnosticProps = {
  latency: {
    client: number;
    server: number;
    total: number;
  };
};
