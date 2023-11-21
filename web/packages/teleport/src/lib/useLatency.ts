/*
Copyright 2020 Gravitational, Inc.

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

import { useState, useEffect } from 'react';

import { EventEmitterWebAuthnSender } from 'teleport/lib/EventEmitterWebAuthnSender';
import { TermEvent } from 'teleport/lib/term/enums';
import { useConsoleContext } from 'teleport/Console/consoleContextProvider';

export default function useLatency(
  emitterSender: EventEmitterWebAuthnSender,
  id: number
) {
  const [state, setState] = useState({
    serverLatency: 0,
    clientLatency: 0,
  });

  const consoleCtx = useConsoleContext();

  const onUpdate = (latencyJSON: string) => {
    const stats = JSON.parse(latencyJSON);
    const client = stats.ws ?? state.clientLatency;
    const server = stats.ssh ?? state.serverLatency;
    consoleCtx.updateSshDocument(id, {
      latency: { client: client, server: server, total: client + server },
    });

    setState({
      ...state,
      serverLatency: server,
      clientLatency: client,
    });
  };

  useEffect(() => {
    if (emitterSender) {
      emitterSender.on(TermEvent.LATENCY, onUpdate);
    }
  }, [emitterSender]);

  return {
    serverLatency: state.serverLatency,
    clientLatency: state.clientLatency,
    setState,
  };
}
