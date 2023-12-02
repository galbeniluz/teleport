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

import React, { useState } from 'react';
import { Flex } from 'design';
import { ButtonPrimary } from 'design/Button';

import { Form, PathInput } from '../CommonElements';

interface DownloadFormProps {
  onAddDownload(sourcePath: string): void;
}

export function DownloadForm(props: DownloadFormProps) {
  const [sourcePath, setSourcePath] = useState('~/');
  const isSourcePathValid = !sourcePath.endsWith('/');

  function download(): void {
    props.onAddDownload(sourcePath);
  }

  return (
    <Form
      onSubmit={e => {
        e.preventDefault();
        download();
      }}
    >
      <Flex alignItems="end">
        <PathInput
          label="File Path"
          autoFocus
          onChange={e => setSourcePath(e.target.value)}
          value={sourcePath}
        />
        <ButtonPrimary
          ml={2}
          px={3}
          size="medium"
          title="Download"
          disabled={!isSourcePathValid}
          type="submit"
        >
          Download
        </ButtonPrimary>
      </Flex>
    </Form>
  );
}
