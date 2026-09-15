// secretref.go resolves container definition `secrets` entries into
// environment variables at task launch, mirroring the AWS ECS agent's own
// secret-injection step. Tarn has no SSM service, so only Secrets Manager
// valueFrom references resolve; an SSM parameter reference fails the launch
// the same way an unreachable Secrets Manager secret does.
package ecs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aircwo-systems/tarn/pkg/types"
)

// SecretsResolver is the narrow slice of Secrets Manager's service the
// runner needs to resolve a container's `secrets` entries. It matches
// (*secrets.Service).GetSecretValue's exact signature, so *secrets.Service
// satisfies it with no adapter. Defined here (the consumer), not depended on
// as a concrete type, the same pattern as taskEngine/logSink above.
type SecretsResolver interface {
	GetSecretValue(nameOrArn string) (*types.Secret, error)
}

// SetSecretsResolver wires the per-account Secrets Manager service secrets
// are resolved from. Safe to leave unset: a container with no `secrets`
// entries never calls it, and one that does gets a clear resolution error
// (rather than a nil-pointer panic) if it was never wired up.
func (r *Runner) SetSecretsResolver(resolver SecretsResolver) { r.secretsResolver = resolver }

// resolveContainerSecrets resolves every entry in cd.Secrets into an
// environment variable value, keyed by its Name. On any failure it returns a
// AWS-shaped ResourceInitializationError referencing the valueFrom that
// failed — never the secret's value, which must not appear in a
// StoppedReason, log line, trace, or Docker label.
func (r *Runner) resolveContainerSecrets(cd types.ContainerDefinition) (map[string]string, error) {
	if len(cd.Secrets) == 0 {
		return nil, nil
	}
	if r.secretsResolver == nil {
		return nil, fmt.Errorf("ResourceInitializationError: unable to pull secrets or registry auth: no secrets resolver configured for this account")
	}

	out := make(map[string]string, len(cd.Secrets))
	for _, secret := range cd.Secrets {
		value, err := r.resolveOneSecret(secret.ValueFrom)
		if err != nil {
			return nil, fmt.Errorf("ResourceInitializationError: unable to pull secrets or registry auth: unable to retrieve secret from valueFrom %q: %w", secret.ValueFrom, err)
		}
		out[secret.Name] = value
	}
	return out, nil
}

// resolveOneSecret resolves a single valueFrom reference. Supported forms:
//   - a bare Secrets Manager secret name
//   - a Secrets Manager ARN: arn:aws:secretsmanager:region:account:secret:NAME-xxxxxx
//   - the same ARN with an appended :jsonKey:versionStage:versionId suffix,
//     which extracts one key out of the secret's JSON SecretString. Blank
//     jsonKey/versionStage/versionId segments mean "use the default"; Tarn
//     keeps only the current version, so versionStage/versionId are accepted
//     and ignored.
//
// An SSM parameter ARN or name is explicitly rejected: Tarn has no SSM
// service.
func (r *Runner) resolveOneSecret(valueFrom string) (string, error) {
	ref := strings.TrimSpace(valueFrom)
	if ref == "" {
		return "", fmt.Errorf("empty valueFrom")
	}
	if strings.HasPrefix(ref, "arn:aws:ssm:") || strings.Contains(ref, ":parameter/") {
		return "", fmt.Errorf("SSM parameters are not supported")
	}

	secretID, jsonKey := splitSecretValueFrom(ref)

	secret, err := r.secretsResolver.GetSecretValue(secretID)
	if err != nil {
		return "", fmt.Errorf("secret not found: %w", err)
	}
	if jsonKey == "" {
		return secret.SecretString, nil
	}
	return extractSecretJSONKey(secret.SecretString, jsonKey)
}

// splitSecretValueFrom separates a Secrets Manager valueFrom into the plain
// secretId Secrets Manager Store.resolve expects and the optional jsonKey
// suffix. A full ARN is "arn:aws:secretsmanager:region:account:secret:name"
// (7 colon-separated segments); anything beyond that is
// ":jsonKey:versionStage:versionId", of which only jsonKey matters here.
// Secret names never contain a colon, so segment count alone tells a bare
// name (1 segment, returned unchanged) apart from a full ARN.
func splitSecretValueFrom(ref string) (secretID, jsonKey string) {
	parts := strings.Split(ref, ":")
	if len(parts) < 7 || parts[0] != "arn" || parts[2] != "secretsmanager" {
		return ref, ""
	}
	secretID = strings.Join(parts[:7], ":")
	if len(parts) >= 8 {
		jsonKey = parts[7]
	}
	return secretID, jsonKey
}

// extractSecretJSONKey pulls one key out of a JSON SecretString, matching
// how ECS resolves a secret with a jsonKey suffix.
func extractSecretJSONKey(secretString, jsonKey string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(secretString), &fields); err != nil {
		return "", fmt.Errorf("secret value is not a JSON object, cannot extract key %q: %w", jsonKey, err)
	}
	raw, ok := fields[jsonKey]
	if !ok {
		return "", fmt.Errorf("secret JSON has no key %q", jsonKey)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	// Non-string JSON values (numbers, booleans) are injected using their
	// literal JSON text, matching how ECS stringifies them into the env var.
	return string(raw), nil
}
