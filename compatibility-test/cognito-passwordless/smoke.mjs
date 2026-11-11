import assert from 'node:assert/strict';
import {
  CognitoIdentityProviderClient, CreateUserPoolCommand, CreateUserPoolClientCommand,
  DeleteUserPoolCommand, SignUpCommand, ConfirmSignUpCommand, InitiateAuthCommand,
  RespondToAuthChallengeCommand, GetUserCommand, ChangePasswordCommand, GlobalSignOutCommand,
} from '@aws-sdk/client-cognito-identity-provider';

// This fixture uses Tarn's local code-inspection API. Never target a live AWS
// pool: delivery and production OTP limits require a separate AWS fixture.
const endpoint = process.env.TARN_ENDPOINT ?? 'http://127.0.0.1:4566';
const sdk = new CognitoIdentityProviderClient({
  endpoint, region: 'us-east-1', credentials: { accessKeyId: 'test', secretAccessKey: 'test' },
});
const send = (Command, input) => sdk.send(new Command(input));
const { UserPool } = await send(CreateUserPoolCommand, {
  PoolName: `passwordless-compat-${Date.now()}`, UserPoolTier: 'ESSENTIALS',
  AutoVerifiedAttributes: ['email'], MfaConfiguration: 'OFF',
  Policies: { SignInPolicy: { AllowedFirstAuthFactors: ['PASSWORD', 'EMAIL_OTP', 'SMS_OTP'] } },
});
const pool = UserPool.Id;
try {
  const { UserPoolClient } = await send(CreateUserPoolClientCommand, {
    UserPoolId: pool, ClientName: 'web',
    ExplicitAuthFlows: ['ALLOW_USER_AUTH', 'ALLOW_REFRESH_TOKEN_AUTH'],
  });
  const ClientId = UserPoolClient.ClientId;
  async function code(username, purpose) {
    const response = await fetch(`${endpoint}/_tarn/admin/cognito/pools/${pool}/users/${username}/codes`);
    assert.equal(response.status, 200);
    const { codes } = await response.json();
    const pending = codes.find(c => c.purpose === purpose);
    assert.ok(pending, `Missing ${purpose} for ${username}`);
    return pending.code;
  }
  for (const factor of ['EMAIL_OTP', 'SMS_OTP']) {
    const Username = factor.toLowerCase();
    await send(SignUpCommand, { ClientId, Username, UserAttributes: [
      { Name: 'email', Value: `${Username}@example.com` },
      { Name: 'phone_number', Value: '+447700900123' },
    ] });
    const choices = await send(InitiateAuthCommand, { ClientId, AuthFlow: 'USER_AUTH', AuthParameters: { USERNAME: Username } });
    assert.equal(choices.ChallengeName, 'SELECT_CHALLENGE');
    assert.deepEqual(choices.AvailableChallenges, ['EMAIL_OTP', 'SMS_OTP']);
    const challenge = await send(RespondToAuthChallengeCommand, {
      ClientId, ChallengeName: 'SELECT_CHALLENGE', Session: choices.Session,
      ChallengeResponses: { USERNAME: Username, ANSWER: factor },
    });
    assert.equal(challenge.ChallengeName, factor);
    const input = { ClientId, Session: challenge.Session, ChallengeName: factor,
      ChallengeResponses: { USERNAME: Username, [`${factor}_CODE`]: await code(Username, `auth:${challenge.Session}`) } };
    const { AuthenticationResult: tokens } = await send(RespondToAuthChallengeCommand, input);
    assert.ok(tokens.IdToken && tokens.AccessToken && tokens.RefreshToken);
    await assert.rejects(send(RespondToAuthChallengeCommand, input), { name: 'NotAuthorizedException' });
    const user = await send(GetUserCommand, { AccessToken: tokens.AccessToken });
    assert.equal(user.UserAttributes.find(a => a.Name === (factor === 'EMAIL_OTP' ? 'email_verified' : 'phone_number_verified')).Value, 'true');
    await send(ChangePasswordCommand, { AccessToken: tokens.AccessToken, ProposedPassword: 'Passw0rd!' });
    const password = await send(InitiateAuthCommand, { ClientId, AuthFlow: 'USER_AUTH', AuthParameters: {
      USERNAME: Username, PREFERRED_CHALLENGE: 'PASSWORD', PASSWORD: 'Passw0rd!',
    } });
    assert.ok(password.AuthenticationResult);
    await send(InitiateAuthCommand, { ClientId, AuthFlow: 'REFRESH_TOKEN_AUTH', AuthParameters: { REFRESH_TOKEN: tokens.RefreshToken } });
    await send(GlobalSignOutCommand, { AccessToken: tokens.AccessToken });
    await assert.rejects(send(InitiateAuthCommand, { ClientId, AuthFlow: 'REFRESH_TOKEN_AUTH', AuthParameters: { REFRESH_TOKEN: tokens.RefreshToken } }), { name: 'NotAuthorizedException' });
  }
  const Username = 'handoff';
  const signup = await send(SignUpCommand, { ClientId, Username, UserAttributes: [{ Name: 'email', Value: 'handoff@example.com' }] });
  const confirmation = await send(ConfirmSignUpCommand, { ClientId, Username, Session: signup.Session, ConfirmationCode: await code(Username, 'signup') });
  assert.ok(confirmation.Session);
  const handoff = await send(InitiateAuthCommand, { ClientId, AuthFlow: 'USER_AUTH', Session: confirmation.Session });
  assert.ok(handoff.AuthenticationResult);
  console.log('PASS: AWS SDK native email/SMS OTP, selection, replay rejection, first password, refresh, sign-out and confirmation handoff');
} finally {
  await send(DeleteUserPoolCommand, { UserPoolId: pool });
  sdk.destroy();
}
