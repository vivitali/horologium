#!/usr/bin/env bash
#
# deploy/bootstrap.sh — one-time AWS setup for SSM-backed state.
#
# Run this ONCE by an operator whose credentials may modify IAM (the CI deploy
# role intentionally cannot). It is idempotent: safe to re-run.
#
# It does what deploy/deploy.sh cannot:
#   1. Attaches an inline SSM read/write policy for both parameters to the
#      shared Lambda execution role.
#   2. Creates the SSM parameters (annual events list, meetup state) if they
#      do not yet exist.
#
# Requirements:
#   - AWS CLI v2 configured with IAM + SSM permissions
#
# Usage:
#   ./deploy/bootstrap.sh
set -euo pipefail

REGION="${AWS_REGION:-us-east-1}"
ROLE_NAME="winnipeg-tech-events-lambda-role"
SSM_PARAM="/winnipeg-tech-events/annual-events"
MEETUP_SSM_PARAM="/winnipeg-tech-events/meetup-state"

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
# One policy covers prod and every stage: /winnipeg-tech-events/*, /winnipeg-tech-events-dev/*, ...
SSM_PARAMS_ARN="arn:aws:ssm:${REGION}:${ACCOUNT_ID}:parameter/winnipeg-tech-events*"

echo "== Attaching SSM read/write policy to ${ROLE_NAME} =="
aws iam put-role-policy \
    --role-name "$ROLE_NAME" \
    --policy-name "ssm-parameters" \
    --policy-document "{
        \"Version\": \"2012-10-17\",
        \"Statement\": [{
            \"Effect\": \"Allow\",
            \"Action\": [\"ssm:GetParameter\", \"ssm:PutParameter\"],
            \"Resource\": \"${SSM_PARAMS_ARN}\"
        }]
    }" >/dev/null
echo "  -> policy ssm-parameters attached (the older ssm-annual-events policy, if any, is left in place)"

ensure_param() {
    local name="$1" seed="$2" label="$3"
    echo "== Ensuring SSM parameter ${name} =="
    if ! aws ssm get-parameter --name "$name" --region "$REGION" >/dev/null 2>&1; then
        aws ssm put-parameter \
            --name "$name" \
            --type String \
            --value "$seed" \
            --region "$REGION" >/dev/null
        echo "  -> created (${label})"
    else
        echo "  -> exists; leaving value untouched"
    fi
}
ensure_param "$SSM_PARAM" '[]' "empty list"
ensure_param "$MEETUP_SSM_PARAM" '{}' "empty state"
# Dev stage parameters (harmless if you never deploy dev).
ensure_param "/winnipeg-tech-events-dev/annual-events" '[]' "empty list, dev"
ensure_param "/winnipeg-tech-events-dev/meetup-state" '{}' "empty state, dev"

echo
echo "== Bootstrap done =="
echo "Now push to deploy the Lambda code + EventBridge rules, then trigger the first annual scrape:"
echo "  aws lambda invoke --function-name winnipeg-tech-events \\"
echo "      --payload '{\"action\":\"scrape_annual\"}' \\"
echo "      --cli-binary-format raw-in-base64-out /tmp/out.json && cat /tmp/out.json"
echo "and dry-run the meetup bot on a no-op day:"
echo "  aws lambda invoke --function-name winnipeg-meetup-bot \\"
echo "      --payload '{\"date\":\"2026-01-02\"}' \\"
echo "      --cli-binary-format raw-in-base64-out /tmp/out.json && cat /tmp/out.json"
