// Copyright (c) glassbox Authors.
// SPDX-License-Identifier: Apache-2.0

import { createHash } from "crypto";
import { Command } from "commander";
import * as dotenv from "dotenv";
import * as fs from "fs";
import stringify from "fast-json-stable-stringify";
import { AuditLogger } from "../audit/AuditLogger";
import { renderAuditHTML, writeAuditReport } from "../audit/AuditRenderer";
import { createAuditSigner } from "../audit/signing/factory";
import { verifyAuditLog } from "../audit/AuditVerifier";
import { ExitCode } from "../exit-codes";

// Load env for key/provider configuration
dotenv.config();

/**
 * Audit command that supports software (Ed25519), PKCS#11, and AWS KMS signing.
 *
 * Provider selection:
 *   --hsm-provider software   (default) local Ed25519 PKCS#8 PEM key
 *   --hsm-provider pkcs11     PKCS#11 HSM via pkcs11js (see PKCS#11 env vars)
 *   --hsm-provider kms        AWS KMS asymmetric key (see KMS env vars)
 *
 * KMS env vars:
 *   GLASSBOX_KMS_KEY_ID             KMS key ID or ARN
 *   AWS_REGION                  AWS region
 *   GLASSBOX_KMS_SIGNING_ALGORITHM  KMS algorithm (default: ECDSA_SHA_256)
 */
export function registerAuditCommands(program: Command): void {
  program
    .command("audit:sign")
    .description("Generate a signed audit log from a JSON payload")
    .requiredOption(
      "--payload <json>",
      "JSON string to sign as the audit trace",
    )
    .option(
      "--hsm-provider <provider>",
      "Signing provider: software (default), pkcs11, or kms",
    )
    .option(
      "--software-private-key <pem>",
      "Ed25519 private key (PKCS#8 PEM). If unset, uses GLASSBOX_AUDIT_PRIVATE_KEY_PEM",
    )
    .option(
      "--kms-key-id <id>",
      "AWS KMS key ID or ARN. If unset, uses GLASSBOX_KMS_KEY_ID",
    )
    .option(
      "--kms-signing-algorithm <alg>",
      "AWS KMS signing algorithm (default: ECDSA_SHA_256). If unset, uses GLASSBOX_KMS_SIGNING_ALGORITHM",
    )
    .option(
      "--dry-run",
      "Validate payload parsing, canonicalization, and provider connectivity without signing",
    )
    .action(async (opts: {
      payload: string;
      hsmProvider?: string;
      softwarePrivateKey?: string;
      kmsKeyId?: string;
      kmsSigningAlgorithm?: string;
      dryRun?: boolean;
    }) => {
      let trace: unknown;
      try {
        trace = JSON.parse(opts.payload);
      } catch {
        console.error('[FAIL] audit signing failed: --payload is not valid JSON.');
        console.error('       Ensure the value is a valid JSON string, e.g.:');
        console.error('         --payload (JSON with input, state, events, timestamp fields)');
        process.exit(ExitCode.VALIDATION_ERROR);
        return;
      }

      const providerLabel = opts.hsmProvider ?? 'software';
      let signer: any = null;

      try {
        signer = createAuditSigner({
          hsmProvider: opts.hsmProvider,
          softwarePrivateKeyPem: opts.softwarePrivateKey ?? process.env.GLASSBOX_AUDIT_PRIVATE_KEY_PEM,
          kmsKeyId: opts.kmsKeyId,
          kmsSigningAlgorithm: opts.kmsSigningAlgorithm,
        });

        if (opts.dryRun) {
          const canonical = stringify(trace);
          const canonicalHash = createHash('sha256').update(canonical as string).digest('hex');
          process.stdout.write(JSON.stringify({
            dry_run: true,
            signer_provider: providerLabel,
            canonical_hash: canonicalHash,
          }, null, 2) + '\n');
          return;
        }

        const logger = new AuditLogger(signer, providerLabel);
        const log = await logger.generateLog(trace as import('../audit/AuditLogger').ExecutionTrace);
        process.stdout.write(JSON.stringify(log, null, 2) + '\n');

      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e);
        console.error(`[FAIL] audit signing failed: ${msg}`);
        if (msg.includes('schema validation failed')) {
          console.error('       Fix the payload fields listed above and retry.');
          console.error('       Required fields: timestamp (ISO 8601), input (object), state (object), events (array).');
          process.exit(ExitCode.VALIDATION_ERROR);
        } else if (msg.includes('private key') || msg.includes('GLASSBOX_AUDIT_PRIVATE_KEY_PEM')) {
          console.error('       Set the GLASSBOX_AUDIT_PRIVATE_KEY_PEM environment variable or pass --software-private-key.');
          process.exit(ExitCode.CONFIGURATION_ERROR);
        } else if (msg.includes('KMS') || msg.includes('kms')) {
          console.error('       Check GLASSBOX_KMS_KEY_ID and AWS_REGION environment variables.');
          process.exit(ExitCode.CONFIGURATION_ERROR);
        } else {
          process.exit(ExitCode.UNKNOWN_ERROR);
        }
      } finally {
        if (signer && typeof signer.close === 'function') {
          try {
            await signer.close();
          } catch (closeError) {
            const msg = closeError instanceof Error ? closeError.message : String(closeError);
            console.error(`[WARN] audit signing cleanup failed: ${msg}`);
          }
        }
      }
    });

  program
    .command("audit:render")
    .description(
      "Render a raw ExecutionTrace or SignedAuditLog JSON payload to an HTML report",
    )
    .requiredOption(
      "--payload <json>",
      "JSON string containing the audit payload (ExecutionTrace or SignedAuditLog)",
    )
    .option("--output <path>", "Write HTML to this file instead of stdout")
    .option("--title <title>", 'Report title (default: "Audit Report")')
    .action((opts: { payload: string; output?: string; title?: string }) => {
      try {
        const payload = JSON.parse(opts.payload);

        if (opts.output) {
          writeAuditReport(payload, opts.output, opts.title);
          console.error(`[OK] Audit report written to ${opts.output}`);
        } else {
          process.stdout.write(renderAuditHTML(payload, opts.title));
        }
      } catch (e) {
        const msg = e instanceof Error ? e.message : String(e);
        console.error(`[FAIL] audit render failed: ${msg}`);
      }
    });

  program
    .command("audit:verify")
    .description("Verify an audit log signature locally (offline verification)")
    .option("--payload <json>", "JSON string of the audit trace")
    .option("--sig <hex>", "Hex-encoded signature")
    .option("--pubkey <pem>", "Public key in PEM format")
    .option("--file <path>", "Path to a complete audit log JSON file")
    .action(
      async (opts: {
        payload?: string;
        sig?: string;
        pubkey?: string;
        file?: string;
      }) => {
        try {
          let auditLog: any;

          if (opts.file) {
            const content = fs.readFileSync(opts.file, "utf8");
            auditLog = JSON.parse(content);
          } else if (opts.payload && opts.sig && opts.pubkey) {
            auditLog = {
              trace: JSON.parse(opts.payload),
              signature: opts.sig,
              publicKey: opts.pubkey,
            };

            const canonicalString = stringify(auditLog.trace);
            auditLog.hash = createHash("sha256")
              .update(canonicalString as string)
              .digest("hex");
          } else {
            throw new Error(
              "You must provide either --file or all of (--payload, --sig, --pubkey)",
            );
          }

          const isValid = verifyAuditLog(auditLog);

          if (isValid) {
            console.log(
              "[OK] Verification successful: Signature and integrity verified.",
            );
          } else {
            console.error(
              "[FAIL] Verification failed: Invalid signature or tampered payload.",
            );
            process.exit(ExitCode.SECURITY_ERROR);
          }
        } catch (e) {
          const msg = e instanceof Error ? e.message : String(e);
          console.error(`[FAIL] audit verification failed: ${msg}`);
          if (msg.includes('You must provide either')) {
            process.exit(ExitCode.VALIDATION_ERROR);
          } else {
            process.exit(ExitCode.UNKNOWN_ERROR);
          }
        }
      },
    );
}
