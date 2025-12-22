---
description: CRITICAL SECURITY RULE - Never commit secrets, API keys, passwords, or credentials
---

# 🚨 CRITICAL: NO SECRETS IN CODE - MANDATORY RULE 🚨

## ABSOLUTE PROHIBITIONS

**YOU MUST NEVER:**

1. **Create or modify `.env` files** - These are NEVER committed to git
2. **Hardcode passwords** - No `password = "actual_value"` EVER
3. **Hardcode API keys** - No `api_key = "sk-..."` or `"AIza..."` EVER  
4. **Hardcode tokens** - No bearer tokens, JWT secrets, OAuth tokens
5. **Include private keys** - No `.pem`, `.key`, certificate contents
6. **Commit credential files** - No files containing real secrets

## SAFE PATTERNS TO USE

### Environment Variables (CORRECT)
```go
password := os.Getenv("DB_PASSWORD")
apiKey := os.Getenv("OPENAI_API_KEY")
```

```typescript
const apiKey = process.env.NEXT_PUBLIC_API_KEY;
```

### Example Files (CORRECT)
Create `.env.example` with placeholders:
```
DB_PASSWORD=your_password_here
API_KEY=your_api_key_here
```

### Secret Managers (CORRECT)
```bash
gcloud secrets versions access latest --secret="MY_SECRET"
```

## PRE-COMMIT VERIFICATION

Before ANY commit that touches configuration:
1. Run `git diff --cached` to review staged changes
2. Search for patterns: `password=`, `secret=`, `key=`, `token=`
3. Verify NO actual secret values are present
4. Check file extensions: NO `.env`, `.pem`, `.key` files

## IF YOU NEED TO STORE A SECRET

1. **STOP** - Do not write the secret in code
2. **ASK** the user how they want to handle it
3. **SUGGEST** using environment variables or secret manager
4. **CREATE** a `.env.example` file showing the variable name only

## CONSEQUENCES OF VIOLATION

Committing secrets to a public repository:
- Exposes credentials to the entire internet
- Requires immediate credential rotation
- Can lead to unauthorized access, data breaches, financial loss
- GitGuardian and other scanners will detect and alert

## EMERGENCY RESPONSE

If you accidentally create a file with secrets:
1. **IMMEDIATELY** run `git reset HEAD <file>`
2. **DO NOT** run `git commit`
3. **DELETE** the file or replace secrets with placeholders
4. **INFORM** the user of the mistake
