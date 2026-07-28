# AWS KMS Audit Signing

Glassbox supports AWS Key Management Service (KMS) as an optional signing provider for audit logs, in addition to the built-in software and PKCS#11 providers.

## Overview

AWS KMS provides hardware-level security for audit log signatures. Private keys never leave AWS KMS, and signing operations are performed within the AWS cloud.

## Requirements

- AWS KMS key with Ed25519 key spec
- AWS credentials with `kms:Sign` and `kms:GetPublicKey` permissions
- AWS SDK for Go v2 (automatically included)

## AWS IAM Policy

Your IAM user/role needs the following permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AllowGlassboxSigning",
      "Effect": "Allow",
      "Action": [
        "kms:Sign",
        "kms:GetPublicKey",
        "kms:DescribeKey"
      ],
      "Resource": "arn:aws:kms:REGION:ACCOUNT_ID:key/KEY_ID"
    }
  ]
}
```

## Creating an Ed25519 KMS Key

```bash
aws kms create-key \
  --key-usage SIGN_VERIFY \
  --key-spec ED25519 \
  --description "Glassbox audit signing key"
```

Save the key ID or alias for use with Glassbox.

## Usage

### Via Environment Variables

```bash
export GLASSBOX_SIGNING_PROVIDER=aws-kms
export GLASSBOX_AWS_KMS_KEY_ID=alias/GlassboxAuditKey
export GLASSBOX_AWS_KMS_REGION=us-east-1

# Optional: AWS credentials (or use default credential chain)
export AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE
export AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY

glassbox audit:sign --payload-file data.json
```

### Via Config File

```toml
[audit]
signing_provider = "aws-kms"

[audit.kms]
key_id = "alias/GlassboxAuditKey"
region = "us-east-1"

# Optional: explicit credentials
# aws_access_key_id = "..."
# aws_secret_access_key = "..."
# aws_profile = "default"
```

### Via CLI Flags

```bash
glassbox audit:sign \
  --payload-file data.json \
  --signing-provider aws-kms \
  --audit-log-kms-key-id alias/GlassboxAuditKey \
  --audit-log-kms-region us-east-1
```

## Retry & Idempotency

`Glassbox` wraps the AWS SDK's Sign call in a bounded retry and idempotency layer (Issue 66) so transient KMS failures do not silently double-bill the service and so callers can reason about what happened.

### What is classified as retryable

Only AWS KMS error codes the layer considers safe. Everything else (bad input, access denied, disabled key, validation failures) stops immediately:

| Class | Behaviour |
| --- | --- |
| Retryable (whitelisted codes) | `InternalError`, `ServiceUnavailable`, `ThrottlingException`, `TooManyRequests[Exception]`, `RequestTimeout[Exception]`, `KMSInternalException`, `UnavailableException`, `RequestLimitExceeded`, `ProvisionedThroughputExceededException` and transport/timeout errors | retried within `GLASSBOX_KMS_MAX_RETRIES` |
| Non-retryable | `AccessDeniedException`, `InvalidKeyIdException`, `InvalidGrantException`, `DisabledException`, `ValidationException`, `NotFoundException`, `IncorrectKeyMaterialException`, `DryRunOperationException` | one call, error propagates |
| Unknown / opaque | one call (safe default) | error propagates |

### Idempotency (client-side)

AWS KMS `Sign` is non-deterministic for ECDSA. To prevent duplicate `Sign` calls within a short window — e.g. when automation retries an audit silently — Glassbox caches each successful signature in-process, keyed by `(key id, SHA-256(message))`. A second sign of the same message within the TTL returns the cached value with `idempotencyHit=true` and `attempts=0`, and never contacts KMS.

Tunables:

| Variable | Default | Effect |
| --- | --- | --- |
| `GLASSBOX_KMS_MAX_RETRIES` | `3` | Retries on safe failures; `0` disables retries. |
| `GLASSBOX_KMS_INITIAL_BACKOFF_MS` | `250` | First retry waits this long; subsequent waits double. |
| `GLASSBOX_KMS_MAX_BACKOFF_MS` | `5000` | Upper bound for any single backoff interval. |
| `GLASSBOX_KMS_JITTER_FRACTION` | `0.2` | Proportional jitter (±20 %) on every backoff — keeps concurrent retriers from synchronising. |
| `GLASSBOX_KMS_IDEMPOTENCY_TTL_MS` | `60000` | Signature cache TTL. `0` disables caching. |
| `GLASSBOX_KMS_IDEMPOTENCY_MAX` | `1024` | LRU cap on the cache. |

### Invariants

- The Message bytes passed to KMS are computed once and reused byte-for-byte across every retry attempt. The canonical digest (what you hand to `Sign`) is never recomputed, copied, or mutated mid-loop.
- Log records include only named scalar attributes: `attempt`, `error_code`, `error_class`, `retryable`, `key_ref` (an 8-character suffix of the key id, never the full ARN), `correlation_id`, and `elapsed_ms`. The audit payload, its digest, and the signature bytes are NEVER logged.
- The AWS SDK's built-in retryer is disabled (`aws.NopRetryer`) so a single source of truth owns the retry budget. Layering the SDK's internal retries on top would silently double-spend the retry budget and confuse logging.

### Inspecting a Sign call

If you need to introspect what happened (retries, idempotency hits, error codes) call the richer `SignWithMetadata` / `signWithMetadata` entry point available in both the Go and TypeScript signers:

```text
Go        : meta, err := kmSigner.SignWithMetadata(ctx, digest, "corr-1234")
TypeScript: try { const { signature, meta } = await kmsSigner.signWithMetadata(digest, { correlationId: "corr-1234" }); }
          catch (e) { /* e instanceof KmsSignError, e.meta carries the metadata */ }
```

The two languages have the same metadata fields but slightly different return shapes:

| Language | On success | On failure |
| --- | --- | --- |
| Go | `meta.Signature` populated, `err == nil` | `meta` carries the final attempt's fields, `err` is a wrapped error (use `errors.Is` / `errors.As` to inspect). |
| TypeScript | `result.signature` populated, `result.meta` populated | `signWithMetadata` throws `KmsSignError` whose `.meta` carries the final attempt's fields and whose `.cause` is the inner AWS/network/empty/cancelled error. |

The legacy `Sign()` / `sign()` entry points still exist on both languages and preserve their old signatures; the `KmsSignError.cause` (TS) is the inner error a legacy caller would have received. To unwrap it explicitly, call the metadata method instead.

Metadata fields:

| Field | Meaning |
| --- | --- |
| `signature` / `Signature` | Signature bytes on success. Undefined / nil on error. |
| `attempts` / `Attempts` | Total KMS API calls; `0` on idempotency cache hit. |
| `idempotencyHit` / `IdempotencyHit` | `true` when the cache short-circuited the call. |
| `errorCode` / `ErrorCode` | AWS error code, `NetworkError`, `ContextCancelled`, or `EmptyMessage` on failure. Empty on success. |
| `errorClass` / `ErrorClass` | Coarse category: `api`, `network`, `context`, `input`, `unknown`. |
| `correlationId` / `CorrelationID` | Echo of the caller's id (or empty). |
| `elapsedMs` / `Elapsed` | Wall-clock duration of the whole call. |

## Supported Key Identifiers

| Format | Example |
|--------|---------|
| Key alias | `alias/GlassboxAuditKey` |
| Key ARN | `arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012` |
| Key ID | `12345678-1234-1234-1234-123456789012` |

## Credential Precedence

AWS credentials are resolved in the following order:

1. Explicit credentials in config (`aws_access_key_id` / `aws_secret_access_key`)
2. AWS profile from config (`aws_profile`)
3. Default credential chain (environment → config file → EC2 role → ECS task)

## Error Handling

The KMS provider includes proper error handling for:

- Invalid key IDs
- Missing permissions
- Network/connectivity issues
- Key state issues (pending deletion, disabled, etc.)

## Verification

To verify KMS signing is working:

```bash
# Sign an audit log
glassbox audit:sign --payload-file data.json --audit-log signed-audit.json

# Verify the signature
glassbox audit:verify --audit-log signed-audit.json
```
