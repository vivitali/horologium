#!/usr/bin/env bash
#
# deploy/deploy.sh — Build and deploy both Winnipeg Tech Events Lambdas:
#   winnipeg-tech-events  (weekly digest + monthly annual-events scrape)
#   winnipeg-meetup-bot   (daily tick for the monthly meetup poll)
#
# Requirements:
#   - AWS CLI v2 configured (e.g. `aws login` or `aws configure sso`)
#   - Go 1.24+ installed
#
# Environment (read by the Lambdas; pass via shell or `deploy/.env`):
#   TELEGRAM_BOT_TOKEN          events bot
#   TELEGRAM_CHAT_ID            events bot
#   TELEGRAM_MEETUP_BOT_TOKEN   meetup bot (separate bot, same group)
#   TELEGRAM_MEETUP_CHAT_ID     meetup bot
#   ANTHROPIC_API_KEY           meetup bot (AI poll option)
#   CITY=Winnipeg (default)
#   CATEGORIES=tech (default)
#   PERIOD_DAYS=30 (default)
#   TEST_MODE=false (default)
#   ANNUAL_SSM_PARAM=/winnipeg-tech-events/annual-events (default; set by this script)
#   MEETUP_SSM_PARAM=/winnipeg-tech-events/meetup-state  (default; set by this script)
set -euo pipefail

FUNCTION_NAME="winnipeg-tech-events"
MEETUP_FUNCTION_NAME="winnipeg-meetup-bot"
REGION="${AWS_REGION:-us-east-1}"
ROLE_NAME="winnipeg-tech-events-lambda-role"
RULE_EVENTS="winnipeg-events-weekly"
RULE_ANNUAL="winnipeg-annual-scrape-monthly"
RULE_MEETUP="winnipeg-meetup-daily"
RULE_POLL_LEGACY="winnipeg-poll-monthly"
SSM_PARAM="/winnipeg-tech-events/annual-events"
MEETUP_SSM_PARAM="/winnipeg-tech-events/meetup-state"
RUNTIME="provided.al2023"
HANDLER="bootstrap"
TIMEOUT=300
MEMORY=512
LOG_RETENTION_DAYS=14

cd "$(dirname "$0")/.."

# Load deploy/.env if present (gitignored)
if [[ -f deploy/.env ]]; then
    set -a
    # shellcheck disable=SC1091
    source deploy/.env
    set +a
fi

echo "== Building Go Lambdas =="
build_zip() {
    local pkg="$1" zip="$2"
    GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
        go build -tags lambda.norpc -o bootstrap "$pkg"
    zip -j "$zip" bootstrap >/dev/null
    rm bootstrap
    echo "  -> $zip"
}
build_zip ./cmd/lambda deploy/lambda.zip
build_zip ./cmd/meetup deploy/meetup.zip

ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
ROLE_ARN="arn:aws:iam::${ACCOUNT_ID}:role/${ROLE_NAME}"

echo "== Ensuring IAM role ${ROLE_NAME} =="
if ! aws iam get-role --role-name "$ROLE_NAME" >/dev/null 2>&1; then
    aws iam create-role \
        --role-name "$ROLE_NAME" \
        --assume-role-policy-document '{
            "Version": "2012-10-17",
            "Statement": [{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]
        }' >/dev/null
    aws iam attach-role-policy \
        --role-name "$ROLE_NAME" \
        --policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole
    echo "  -> role created; waiting 10s for IAM propagation"
    sleep 10
fi

# NOTE: the Lambda role's SSM read/write policy and the SSM parameters
# themselves are one-time setup handled by deploy/bootstrap.sh (run by an
# operator with IAM permissions). The CI deploy role is intentionally not
# allowed to call iam:PutRolePolicy, so those steps must not live here.

# deploy_function NAME ZIP "K=V,K=V,..."
deploy_function() {
    local name="$1" zip="$2" env_vars="$3"
    echo "== Deploying Lambda function ${name} =="
    if aws lambda get-function --function-name "$name" --region "$REGION" >/dev/null 2>&1; then
        aws lambda update-function-code \
            --function-name "$name" --zip-file "fileb://${zip}" --region "$REGION" >/dev/null
        aws lambda wait function-updated --function-name "$name" --region "$REGION"
        aws lambda update-function-configuration \
            --function-name "$name" --runtime "$RUNTIME" --handler "$HANDLER" \
            --timeout "$TIMEOUT" --memory-size "$MEMORY" --region "$REGION" >/dev/null
        echo "  -> updated"
    else
        aws lambda create-function \
            --function-name "$name" --runtime "$RUNTIME" --role "$ROLE_ARN" \
            --handler "$HANDLER" --zip-file "fileb://${zip}" \
            --timeout "$TIMEOUT" --memory-size "$MEMORY" --region "$REGION" >/dev/null
        echo "  -> created"
    fi

    echo "== Setting environment variables for ${name} =="
    aws lambda wait function-updated --function-name "$name" --region "$REGION"
    aws lambda update-function-configuration \
        --function-name "$name" --region "$REGION" \
        --environment "Variables={${env_vars}}" >/dev/null

    echo "== Setting CloudWatch log retention for ${name} to ${LOG_RETENTION_DAYS} days =="
    aws logs put-retention-policy \
        --log-group-name "/aws/lambda/${name}" \
        --retention-in-days "$LOG_RETENTION_DAYS" --region "$REGION" 2>/dev/null || true
}

deploy_function "$FUNCTION_NAME" deploy/lambda.zip "
        TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN:-},
        TELEGRAM_CHAT_ID=${TELEGRAM_CHAT_ID:-},
        CITY=${CITY:-Winnipeg},
        CATEGORIES=${CATEGORIES:-tech},
        PERIOD_DAYS=${PERIOD_DAYS:-30},
        TEST_MODE=${TEST_MODE:-false},
        ANNUAL_SSM_PARAM=${SSM_PARAM}
    "

deploy_function "$MEETUP_FUNCTION_NAME" deploy/meetup.zip "
        TELEGRAM_MEETUP_BOT_TOKEN=${TELEGRAM_MEETUP_BOT_TOKEN:-},
        TELEGRAM_MEETUP_CHAT_ID=${TELEGRAM_MEETUP_CHAT_ID:-},
        ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-},
        TEST_MODE=${TEST_MODE:-false},
        MEETUP_SSM_PARAM=${MEETUP_SSM_PARAM}
    "

# create_or_update_rule RULE SCHEDULE INPUT DESCRIPTION FUNCTION
create_or_update_rule() {
    local rule_name="$1" schedule="$2" input="$3" description="$4" fn="$5"

    echo "== Ensuring EventBridge rule ${rule_name} -> ${fn} =="
    aws events put-rule \
        --name "$rule_name" \
        --schedule-expression "$schedule" \
        --description "$description" \
        --region "$REGION" >/dev/null

    aws lambda add-permission \
        --function-name "$fn" \
        --statement-id "allow-${rule_name}" \
        --action lambda:InvokeFunction \
        --principal events.amazonaws.com \
        --source-arn "arn:aws:events:${REGION}:${ACCOUNT_ID}:rule/${rule_name}" \
        --region "$REGION" 2>/dev/null || true

    aws events put-targets \
        --rule "$rule_name" \
        --targets "Id=1,Arn=arn:aws:lambda:${REGION}:${ACCOUNT_ID}:function:${fn},Input='${input}'" \
        --region "$REGION" >/dev/null
}

# Mondays 14:00 UTC (9 AM CST)
create_or_update_rule "$RULE_EVENTS" \
    "cron(0 14 ? * MON *)" \
    '{"action":"events"}' \
    "Weekly Winnipeg tech events digest" \
    "$FUNCTION_NAME"

# 1st of each month, 13:00 UTC — refreshes annual events ahead of the
# weekly digest (Mondays 14:00 UTC).
create_or_update_rule "$RULE_ANNUAL" \
    "cron(0 13 1 * ? *)" \
    '{"action":"scrape_annual"}' \
    "Monthly refresh of curated annual events" \
    "$FUNCTION_NAME"

# Daily 14:00 UTC (08:00/09:00 Winnipeg). The meetup bot decides in code
# whether today is a poll day (last Saturday - 11), a summary day (- 3),
# or nothing.
create_or_update_rule "$RULE_MEETUP" \
    "cron(0 14 * * ? *)" \
    '{}' \
    "Daily tick for the monthly meetup poll bot" \
    "$MEETUP_FUNCTION_NAME"

echo "== Removing legacy rule ${RULE_POLL_LEGACY} (if present) =="
if aws events describe-rule --name "$RULE_POLL_LEGACY" --region "$REGION" >/dev/null 2>&1; then
    aws events remove-targets --rule "$RULE_POLL_LEGACY" --ids 1 --region "$REGION" >/dev/null || true
    aws events delete-rule --name "$RULE_POLL_LEGACY" --region "$REGION" >/dev/null
    aws lambda remove-permission --function-name "$FUNCTION_NAME" \
        --statement-id "allow-${RULE_POLL_LEGACY}" --region "$REGION" 2>/dev/null || true
    echo "  -> deleted"
else
    echo "  -> not present"
fi

echo
echo "== Done =="
echo
echo "Manual invoke:"
echo "  aws lambda invoke --function-name ${FUNCTION_NAME} \\"
echo "      --payload '{\"action\":\"events\"}' --cli-binary-format raw-in-base64-out out.json && cat out.json"
echo "  aws lambda invoke --function-name ${MEETUP_FUNCTION_NAME} \\"
echo "      --payload '{\"date\":\"2026-10-20\"}' --cli-binary-format raw-in-base64-out out.json && cat out.json"
echo
echo "Logs:"
echo "  aws logs tail /aws/lambda/${FUNCTION_NAME} --follow --region ${REGION}"
echo "  aws logs tail /aws/lambda/${MEETUP_FUNCTION_NAME} --follow --region ${REGION}"
