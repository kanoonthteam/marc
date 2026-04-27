---
name: api-security
description: API authentication, authorization, rate limiting, CORS, input validation, and security hardening
---

# API Security Best Practices

## Purpose

Guide agents in implementing robust API security covering authentication, authorization, rate limiting, CORS, input validation, and error handling. Based on OWASP API Security Top 10 (2023) and industry best practices for 2025.

## Authentication Patterns

### Bearer Token Authentication

```typescript
// Middleware to validate Bearer tokens
import { verify } from 'jsonwebtoken';

function authenticate(req, res, next) {
  const authHeader = req.headers.authorization;
  if (!authHeader?.startsWith('Bearer ')) {
    return res.status(401).json({
      type: 'https://api.example.com/errors/unauthorized',
      title: 'Unauthorized',
      status: 401,
      detail: 'Missing or invalid Authorization header. Expected: Bearer <token>',
    });
  }

  const token = authHeader.slice(7);
  try {
    const payload = verify(token, process.env.JWT_SECRET, {
      algorithms: ['HS256'],
      issuer: 'api.example.com',
      audience: 'api.example.com',
    });
    req.user = payload;
    next();
  } catch (error) {
    return res.status(401).json({
      type: 'https://api.example.com/errors/token-expired',
      title: 'Token Expired',
      status: 401,
      detail: 'Access token has expired. Please refresh your token.',
    });
  }
}
```

### JWT Best Practices

```typescript
import { sign, verify } from 'jsonwebtoken';

// Token generation with short expiry
function generateTokens(user) {
  const accessToken = sign(
    {
      sub: user.id,
      email: user.email,
      role: user.role,
    },
    process.env.JWT_SECRET,
    {
      algorithm: 'HS256',
      expiresIn: '15m',      // Short-lived access token
      issuer: 'api.example.com',
      audience: 'api.example.com',
    },
  );

  const refreshToken = sign(
    { sub: user.id, type: 'refresh' },
    process.env.JWT_REFRESH_SECRET,
    {
      algorithm: 'HS256',
      expiresIn: '7d',       // Longer-lived refresh token
      issuer: 'api.example.com',
    },
  );

  return { accessToken, refreshToken };
}

// Refresh token endpoint
app.post('/api/v1/auth/refresh', async (req, res) => {
  const { refreshToken } = req.body;
  if (!refreshToken) {
    return res.status(400).json({ error: 'Refresh token required' });
  }

  try {
    const payload = verify(refreshToken, process.env.JWT_REFRESH_SECRET);

    // Check if refresh token is revoked (stored in DB/Redis)
    const isRevoked = await tokenStore.isRevoked(refreshToken);
    if (isRevoked) {
      return res.status(401).json({ error: 'Refresh token revoked' });
    }

    // Rotate refresh token (one-time use)
    await tokenStore.revoke(refreshToken);

    const user = await db.users.findById(payload.sub);
    const tokens = generateTokens(user);

    // Store new refresh token
    await tokenStore.store(tokens.refreshToken, user.id, '7d');

    res.json(tokens);
  } catch {
    return res.status(401).json({ error: 'Invalid refresh token' });
  }
});
```

**JWT Rules:**
- Access token expiry: 5-15 minutes
- Refresh token expiry: 7-30 days
- Rotate refresh tokens on each use (one-time use)
- Store refresh tokens in httpOnly cookies or secure storage
- Include minimal claims (sub, role) -- do not store sensitive data
- Use asymmetric keys (RS256/ES256) for multi-service architectures
- Always validate `iss`, `aud`, `exp` claims

### API Key Authentication

```typescript
// API key authentication for service-to-service or external integrations
function authenticateApiKey(req, res, next) {
  const apiKey = req.headers['x-api-key'];
  if (!apiKey) {
    return res.status(401).json({ error: 'API key required' });
  }

  // Hash the API key before lookup (never store plaintext)
  const hashedKey = crypto.createHash('sha256').update(apiKey).digest('hex');
  const keyRecord = await db.apiKeys.findByHash(hashedKey);

  if (!keyRecord || keyRecord.revokedAt) {
    return res.status(401).json({ error: 'Invalid API key' });
  }

  // Check expiry
  if (keyRecord.expiresAt && keyRecord.expiresAt < new Date()) {
    return res.status(401).json({ error: 'API key expired' });
  }

  req.apiClient = keyRecord;
  next();
}
```

**API Key Rules:**
- Hash keys before storing (like passwords)
- Support key rotation (multiple active keys per client)
- Include expiration dates
- Log key usage for audit trail
- Use prefix format for identification: `sk_live_abc123`, `sk_test_def456`

### OAuth2 Flows

| Flow | Use Case | Security Level |
|------|----------|---------------|
| Authorization Code + PKCE | Web apps, mobile apps, SPAs | High |
| Client Credentials | Machine-to-machine, service accounts | High |
| Device Code | Smart TVs, CLI tools, IoT devices | Medium |
| ~~Implicit~~ | Deprecated -- use Auth Code + PKCE instead | Low |
| ~~Resource Owner Password~~ | Deprecated -- legacy apps only | Low |

```typescript
// Authorization Code + PKCE flow (recommended for all client types)
// Step 1: Generate PKCE challenge
import { randomBytes, createHash } from 'crypto';

function generatePKCE() {
  const verifier = randomBytes(32).toString('base64url');
  const challenge = createHash('sha256').update(verifier).digest('base64url');
  return { verifier, challenge };
}

// Step 2: Redirect to authorization server
const authUrl = new URL('https://auth.example.com/authorize');
authUrl.searchParams.set('response_type', 'code');
authUrl.searchParams.set('client_id', CLIENT_ID);
authUrl.searchParams.set('redirect_uri', 'https://app.example.com/callback');
authUrl.searchParams.set('scope', 'read write');
authUrl.searchParams.set('state', randomBytes(16).toString('hex'));
authUrl.searchParams.set('code_challenge', pkce.challenge);
authUrl.searchParams.set('code_challenge_method', 'S256');
```

## Rate Limiting

### Token Bucket Algorithm

```typescript
import { RateLimiterRedis } from 'rate-limiter-flexible';
import Redis from 'ioredis';

const redis = new Redis(process.env.REDIS_URL);

// Different limits for different tiers
const rateLimiters = {
  free: new RateLimiterRedis({
    storeClient: redis,
    keyPrefix: 'rl:free',
    points: 100,        // 100 requests
    duration: 60,        // per 60 seconds
    blockDuration: 60,   // Block for 60 seconds when exceeded
  }),
  pro: new RateLimiterRedis({
    storeClient: redis,
    keyPrefix: 'rl:pro',
    points: 1000,
    duration: 60,
  }),
};

async function rateLimitMiddleware(req, res, next) {
  const tier = req.user?.tier || 'free';
  const key = req.user?.id || req.ip;
  const limiter = rateLimiters[tier] || rateLimiters.free;

  try {
    const result = await limiter.consume(key);

    // Always include rate limit headers
    res.set({
      'X-RateLimit-Limit': limiter.points,
      'X-RateLimit-Remaining': result.remainingPoints,
      'X-RateLimit-Reset': new Date(Date.now() + result.msBeforeNext).toISOString(),
    });

    next();
  } catch (rejRes) {
    const retryAfter = Math.ceil(rejRes.msBeforeNext / 1000);
    res.set({
      'X-RateLimit-Limit': limiter.points,
      'X-RateLimit-Remaining': 0,
      'X-RateLimit-Reset': new Date(Date.now() + rejRes.msBeforeNext).toISOString(),
      'Retry-After': retryAfter,
    });

    return res.status(429).json({
      type: 'https://api.example.com/errors/rate-limit-exceeded',
      title: 'Too Many Requests',
      status: 429,
      detail: `Rate limit exceeded. Retry after ${retryAfter} seconds.`,
      retryAfter,
    });
  }
}
```

### Rate Limiting Strategies

| Strategy | Description | Best For |
|----------|-------------|----------|
| Fixed Window | Count requests per time window | Simple, low overhead |
| Sliding Window | Rolling count over time period | More accurate, slightly more complex |
| Token Bucket | Refill tokens at steady rate | Allows bursts, smooths traffic |
| Leaky Bucket | Process at fixed rate, queue excess | Consistent throughput |

### Rate Limit Headers (RFC 7231 + Draft Standard)

```
X-RateLimit-Limit: 100          # Max requests per window
X-RateLimit-Remaining: 42       # Requests remaining
X-RateLimit-Reset: 2025-01-15T10:31:00Z  # When limit resets (ISO 8601)
Retry-After: 30                  # Seconds to wait (on 429 response)
```

## CORS Configuration

```typescript
import cors from 'cors';

// Production CORS configuration
const corsOptions = {
  origin: (origin, callback) => {
    const allowedOrigins = [
      'https://app.example.com',
      'https://admin.example.com',
    ];

    // Allow requests with no origin (mobile apps, server-to-server)
    if (!origin || allowedOrigins.includes(origin)) {
      callback(null, true);
    } else {
      callback(new Error('Not allowed by CORS'));
    }
  },
  methods: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'],
  allowedHeaders: ['Content-Type', 'Authorization', 'X-Request-ID'],
  exposedHeaders: ['X-RateLimit-Limit', 'X-RateLimit-Remaining', 'X-RateLimit-Reset'],
  credentials: true,           // Allow cookies
  maxAge: 86400,               // Cache preflight for 24 hours
};

app.use(cors(corsOptions));
```

**CORS Rules:**
- Never use `origin: '*'` with `credentials: true`
- Whitelist specific origins, not wildcard patterns
- Expose rate limit headers so clients can read them
- Set appropriate `maxAge` to reduce preflight requests
- In development, you may allow `localhost` origins

## RFC 7807 Problem Details for Errors

Standardized error format adopted by major APIs.

```typescript
// Error response factory
function problemDetails(status, title, detail, extras = {}) {
  return {
    type: `https://api.example.com/errors/${title.toLowerCase().replace(/\s+/g, '-')}`,
    title,
    status,
    detail,
    instance: extras.instance,
    ...extras,
  };
}

// Validation error with field details
app.use((err, req, res, next) => {
  if (err.name === 'ValidationError') {
    return res.status(422).json(problemDetails(422, 'Validation Failed', err.message, {
      instance: req.originalUrl,
      errors: err.details.map(d => ({
        field: d.path,
        message: d.message,
        code: d.type.toUpperCase(),
      })),
    }));
  }

  if (err.name === 'NotFoundError') {
    return res.status(404).json(problemDetails(404, 'Not Found', err.message, {
      instance: req.originalUrl,
    }));
  }

  // Catch-all: never leak internal details
  console.error(err);
  return res.status(500).json(problemDetails(500, 'Internal Server Error',
    'An unexpected error occurred. Please try again later.', {
      instance: req.originalUrl,
      // Include correlation ID for debugging
      correlationId: req.headers['x-correlation-id'],
    },
  ));
});
```

## Input Validation

### Zod Schema Validation (Recommended for TypeScript)

```typescript
import { z } from 'zod';

const CreateBookmarkSchema = z.object({
  url: z.string().url('Must be a valid URL').max(2048),
  title: z.string().min(1, 'Title is required').max(200).trim(),
  description: z.string().max(1000).trim().optional(),
  tags: z.array(z.string().max(50).trim()).max(20).optional().default([]),
});

// Validation middleware factory
function validate(schema) {
  return (req, res, next) => {
    const result = schema.safeParse(req.body);
    if (!result.success) {
      return res.status(422).json({
        type: 'https://api.example.com/errors/validation-failed',
        title: 'Validation Failed',
        status: 422,
        detail: 'One or more fields failed validation.',
        errors: result.error.issues.map(issue => ({
          field: issue.path.join('.'),
          message: issue.message,
          code: issue.code,
        })),
      });
    }
    req.validatedBody = result.data;
    next();
  };
}

app.post('/api/v1/bookmarks', validate(CreateBookmarkSchema), async (req, res) => {
  const bookmark = await createBookmark(req.validatedBody);
  res.status(201).json(bookmark);
});
```

### Input Validation Rules

1. **Validate at the API boundary** -- before any business logic
2. **Whitelist allowed fields** -- reject unknown properties
3. **Enforce type constraints** -- numbers, strings, dates
4. **Set maximum lengths** -- prevent oversized payloads
5. **Sanitize strings** -- trim whitespace, normalize Unicode
6. **Validate enums** -- reject unknown values
7. **Never trust client data** -- re-validate on the server even if validated on client

## Request Signing (Webhook Security)

```typescript
import { createHmac, timingSafeEqual } from 'crypto';

// Signing outgoing webhooks
function signPayload(payload, secret) {
  const timestamp = Math.floor(Date.now() / 1000);
  const signature = createHmac('sha256', secret)
    .update(`${timestamp}.${JSON.stringify(payload)}`)
    .digest('hex');
  return { signature: `v1=${signature}`, timestamp };
}

// Verifying incoming webhooks
function verifyWebhook(req, res, next) {
  const signature = req.headers['x-webhook-signature'];
  const timestamp = parseInt(req.headers['x-webhook-timestamp']);

  // Reject if timestamp is too old (5 minute window)
  if (Math.abs(Date.now() / 1000 - timestamp) > 300) {
    return res.status(401).json({ error: 'Webhook timestamp too old' });
  }

  const expected = createHmac('sha256', process.env.WEBHOOK_SECRET)
    .update(`${timestamp}.${JSON.stringify(req.body)}`)
    .digest('hex');

  const expectedBuffer = Buffer.from(`v1=${expected}`);
  const receivedBuffer = Buffer.from(signature);

  // Timing-safe comparison to prevent timing attacks
  if (expectedBuffer.length !== receivedBuffer.length ||
      !timingSafeEqual(expectedBuffer, receivedBuffer)) {
    return res.status(401).json({ error: 'Invalid webhook signature' });
  }

  next();
}
```

## Security Headers

```typescript
import helmet from 'helmet';

app.use(helmet({
  contentSecurityPolicy: {
    directives: {
      defaultSrc: ["'self'"],
      scriptSrc: ["'self'"],
      styleSrc: ["'self'", "'unsafe-inline'"],
      imgSrc: ["'self'", 'data:', 'https:'],
    },
  },
  crossOriginEmbedderPolicy: true,
  crossOriginOpenerPolicy: true,
  crossOriginResourcePolicy: { policy: 'same-origin' },
  hsts: { maxAge: 31536000, includeSubDomains: true, preload: true },
  noSniff: true,
  referrerPolicy: { policy: 'strict-origin-when-cross-origin' },
}));
```

## Best Practices

1. **Use HTTPS everywhere** -- redirect HTTP to HTTPS, set HSTS
2. **Short-lived access tokens** (5-15 minutes) with refresh token rotation
3. **Hash API keys** before storing -- never store plaintext
4. **Rate limit all endpoints** -- different tiers for different clients
5. **Validate all input** at the API boundary with strict schemas
6. **Never expose internal errors** -- log details server-side, return generic messages
7. **Use timing-safe comparison** for signature verification
8. **Implement request signing** for webhooks
9. **Log authentication failures** for security monitoring
10. **Rotate secrets regularly** and support multiple active keys during rotation

## Anti-Patterns

- **Storing JWTs in localStorage** -- use httpOnly cookies instead (prevents XSS theft)
- **Long-lived access tokens** (hours or days) -- use short expiry with refresh
- **Wildcard CORS** (`*`) with credentials -- explicitly whitelist origins
- **Trusting client-side validation** -- always re-validate server-side
- **Logging sensitive data** -- never log tokens, passwords, or PII
- **Using MD5 or SHA1** for hashing -- use SHA256+ or bcrypt/argon2 for passwords
- **Rate limiting by IP only** -- authenticated users should be limited by user ID
- **Returning different errors for "user not found" vs "wrong password"** -- use generic "invalid credentials"

## OWASP API Security Top 10 (2023) Checklist

| Risk | Mitigation |
|------|-----------|
| API1: Broken Object Level Authorization | Check ownership on every resource access |
| API2: Broken Authentication | Short-lived tokens, MFA, account lockout |
| API3: Broken Object Property Level Authorization | Whitelist response fields, validate input fields |
| API4: Unrestricted Resource Consumption | Rate limiting, payload size limits, pagination limits |
| API5: Broken Function Level Authorization | Role-based middleware on every endpoint |
| API6: Unrestricted Access to Sensitive Business Flows | CAPTCHA, bot detection on critical flows |
| API7: Server-Side Request Forgery (SSRF) | Validate URLs, block internal IPs, use allowlists |
| API8: Security Misconfiguration | Helmet headers, CORS config, disable debug mode |
| API9: Improper Inventory Management | Document all endpoints, sunset old versions |
| API10: Unsafe Consumption of APIs | Validate responses from third-party APIs |

## Sources & References

- OWASP API Security Top 10 (2023): https://owasp.org/API-Security/editions/2023/en/0x11-t10/
- RFC 7807 Problem Details for HTTP APIs: https://datatracker.ietf.org/doc/html/rfc7807
- OAuth 2.0 Security Best Current Practice: https://datatracker.ietf.org/doc/html/draft-ietf-oauth-security-topics
- JWT Best Practices (RFC 8725): https://datatracker.ietf.org/doc/html/rfc8725
- Helmet.js Security Headers: https://helmetjs.github.io/
- Zod Validation Library: https://zod.dev/
- Rate Limiting Best Practices: https://cloud.google.com/architecture/rate-limiting-strategies-techniques
