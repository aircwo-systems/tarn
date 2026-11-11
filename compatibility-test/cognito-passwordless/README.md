# Cognito passwordless SDK smoke test

Start Tarn, then run:

```sh
npm ci
TARN_ENDPOINT=http://127.0.0.1:4566 npm test
```

The fixture uses the real AWS SDK v3 and creates an isolated pool, then deletes it in a `finally` block. It covers email/SMS OTP selection and completion, replay rejection, contact verification, first-password setup, refresh, global sign-out and sign-up confirmation handoff.

It reads OTPs through Tarn's local pending-code API. This test targets Tarn only. Live AWS comparison needs an authorized pool and access to its actual delivery channel; it must verify OTP format, lifetime, retry behavior and concurrent-attempt invalidation separately.
