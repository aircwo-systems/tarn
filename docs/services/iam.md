# IAM

Identity and Access Management stubs for Terraform compatibility.

<span class="status-badge status-stub">Stub</span>

IAM in Tarn provides minimal role and policy management to keep Terraform workflows running. It does not enforce permissions — all IAM actions succeed, and roles are auto-created on first reference.

## Supported Operations

| Operation | Status | Notes |
|-----------|--------|-------|
| CreateRole | Supported | Stores role in memory |
| GetRole | Supported | Auto-creates stub if not found |
| DeleteRole | Supported | |
| UpdateRole | Supported | Empty OK |
| UpdateRoleDescription | Supported | Empty OK |
| UpdateAssumeRolePolicy | Supported | Empty OK |
| TagRole | Supported | Empty OK |
| UntagRole | Supported | Empty OK |
| ListRoleTags | Supported | |
| AttachRolePolicy | Supported | Tracks attachment |
| DetachRolePolicy | Supported | Tracks detachment |
| ListAttachedRolePolicies | Supported | |
| PutRolePolicy | Supported | Stores inline policy |
| GetRolePolicy | Supported | |
| DeleteRolePolicy | Supported | |
| ListRolePolicies | Supported | |
| CreateInstanceProfile | Supported | |
| GetInstanceProfile | Supported | |
| DeleteInstanceProfile | Supported | Empty OK |
| AddRoleToInstanceProfile | Supported | Empty OK |
| RemoveRoleFromInstanceProfile | Supported | Empty OK |
| ListInstanceProfilesForRole | Supported | |
| PassRole | Supported | Empty OK |

::: tip Lenient Default
Any IAM action not listed above still returns `200 OK` with an empty XML result. This means Terraform IAM resources that call actions Tarn doesn't explicitly handle will succeed silently. Check logs for `[iam] unhandled action` entries.
:::

## Terraform Usage

Terraform configurations typically reference IAM roles by ARN. Tarn accepts any role ARN — if the role doesn't exist in memory, it's auto-created with stub data on first `GetRole`.

```hcl
resource "aws_iam_role" "lambda_exec" {
  name = "lambda-exec"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda_exec.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

## Protocol

- **Protocol:** Query/XML (`POST /` with `Action=...` and `Version=2010-05-08`)
- **Routing:** Detected by `Version` form parameter, not action name
- **Namespace:** `https://iam.amazonaws.com/doc/2010-05-08/`

## Known Limitations

- No permission enforcement — all actions are allowed regardless of policy
- No STS support (AssumeRole, GetCallerIdentity, etc.)
- Role trust policies are stored but not evaluated
- Instance profiles are tracked minimally
