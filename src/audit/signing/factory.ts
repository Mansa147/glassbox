// Copyright (c) glassbox Authors.
// SPDX-License-Identifier: Apache-2.0

import type { AuditSigner } from './types';
import { SoftwareEd25519Signer } from './softwareSigner';
import { Pkcs11Signer } from './pkcs11Signer';
import { KmsSigner } from './kmsSigner';
import { loadKmsRetryConfigFromEnv, type KmsRetryConfig } from './kmsRetry';

export type SigningProvider = 'software' | 'pkcs11' | 'kms';

export interface CreateAuditSignerOpts {
  hsmProvider?: string;
  softwarePrivateKeyPem?: string;
  kmsKeyId?: string;
  kmsSigningAlgorithm?: string;
  /** Optional per-instance overrides for KMS retry policy. */
  kmsRetryConfig?: Partial<KmsRetryConfig>;
}

export function createAuditSigner(opts: CreateAuditSignerOpts): AuditSigner {
  const provider = (opts.hsmProvider?.toLowerCase() ?? 'software') as SigningProvider;

  switch (provider) {
    case 'kms':
      // Pass the resolved retry config so the KmsSigner applies
      // GLASSBOX_KMS_* env vars on top of the production defaults.
      return new KmsSigner({
        keyId: opts.kmsKeyId,
        signingAlgorithm: opts.kmsSigningAlgorithm,
        retryConfig: {
          ...loadKmsRetryConfigFromEnv(),
          ...(opts.kmsRetryConfig ?? {}),
        },
      });

    case 'pkcs11':
      // The Pkcs11Signer handles algorithm choice via GLASSBOX_PKCS11_ALGORITHM env var
      return new Pkcs11Signer();

    case 'software':
      if (!opts.softwarePrivateKeyPem) {
        throw new Error('software signing selected but no private key was provided');
      }
      return new SoftwareEd25519Signer(opts.softwarePrivateKeyPem);

    default:
      throw new Error(`unknown signing provider: "${provider}". Valid options: software, pkcs11, kms`);
  }
}
