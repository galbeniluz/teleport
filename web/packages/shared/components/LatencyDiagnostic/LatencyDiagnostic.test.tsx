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

import {
  latencyColors,
  errorThreshold,
  latencyColor,
  warnThreshold,
} from 'shared/components/LatencyDiagnostic/LatencyDiagnostic';

test('latency colors', () => {
  // green + green = green
  expect(latencyColors(warnThreshold - 1, warnThreshold - 1)).toStrictEqual({
    client: latencyColor.ok,
    server: latencyColor.ok,
    total: latencyColor.ok,
  });
  // green + yellow = yellow
  expect(latencyColors(warnThreshold - 1, warnThreshold)).toStrictEqual({
    client: latencyColor.ok,
    server: latencyColor.warn,
    total: latencyColor.warn,
  });
  expect(latencyColors(warnThreshold, warnThreshold - 1)).toStrictEqual({
    client: latencyColor.warn,
    server: latencyColor.ok,
    total: latencyColor.warn,
  });
  // green + red = red
  expect(latencyColors(warnThreshold - 1, errorThreshold)).toStrictEqual({
    client: latencyColor.ok,
    server: latencyColor.error,
    total: latencyColor.error,
  });
  expect(latencyColors(errorThreshold, warnThreshold - 1)).toStrictEqual({
    client: latencyColor.error,
    server: latencyColor.ok,
    total: latencyColor.error,
  });
  // yellow + yellow = yellow
  expect(latencyColors(warnThreshold, warnThreshold)).toStrictEqual({
    client: latencyColor.warn,
    server: latencyColor.warn,
    total: latencyColor.warn,
  });
  // yellow + red = red
  expect(latencyColors(warnThreshold, errorThreshold)).toStrictEqual({
    client: latencyColor.warn,
    server: latencyColor.error,
    total: latencyColor.error,
  });
  expect(latencyColors(errorThreshold, warnThreshold)).toStrictEqual({
    client: latencyColor.error,
    server: latencyColor.warn,
    total: latencyColor.error,
  });
  // red + red = red
  expect(latencyColors(errorThreshold, errorThreshold)).toStrictEqual({
    client: latencyColor.error,
    server: latencyColor.error,
    total: latencyColor.error,
  });
});
