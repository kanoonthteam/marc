---
name: security-architecture
description: Zero-trust architecture, STRIDE threat modeling, OAuth2/OIDC, encryption, API security, and compliance
---

# Security Architecture

## Overview

Security architecture defines how security controls are implemented across system layers to protect data, services, and users. Modern security follows zero-trust principles, defense-in-depth strategies, and compliance-driven design. This skill covers threat modeling, authentication/authorization patterns, encryption, API security, and compliance frameworks.

## Zero-Trust Principles

### Core Tenets

1. **Never trust, always verify** -- authenticate and authorize every request regardless of origin
2. **Least privilege** -- grant only the minimum permissions needed for the task
3. **Assume breach** -- design systems as if an attacker is already inside the network

### Zero-Trust Architecture Components

```
┌─────────────────────────────────────────────────────────┐
│                    Zero-Trust Control Plane              │
│                                                          │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │ Identity     │  │ Policy       │  │ Continuous    │  │
│  │ Provider     │  │ Engine       │  │ Monitoring    │  │
│  │ (IdP)        │  │ (OPA/Cedar)  │  │ (SIEM)       │  │
│  └──────┬──────┘  └──────┬───────┘  └──────┬────────┘  │
│         │                │                  │           │
└─────────┼────────────────┼──────────────────┼───────────┘
          │                │                  │
          ▼                ▼                  ▼
┌─────────────────────────────────────────────────────────┐
│                    Data Plane                            │
│                                                          │
│  User ──→ [MFA] ──→ [Device Check] ──→ [Policy Check]  │
│                                              │           │
│                                              ▼           │
│           ┌──────────┐    ┌──────────┐    ┌──────────┐  │
│           │ Service A │───→│ Service B │───→│ Database │  │
│           │ [mTLS]   │    │ [mTLS]   │    │ [encrypt]│  │
│           └──────────┘    └──────────┘    └──────────┘  │
│                                                          │
│  Every request: authenticated + authorized + encrypted   │
└──────────────────────────────────────────────────────────┘
```

### Implementation Checklist

```markdown
## Zero-Trust Implementation Checklist

### Identity
- [ ] Centralized identity provider (Okta, Azure AD, Auth0)
- [ ] Multi-factor authentication (MFA) for all users
- [ ] Service-to-service authentication (mTLS or JWT)
- [ ] Short-lived credentials (tokens expire in < 1 hour)
- [ ] Just-in-time (JIT) access for elevated permissions

### Network
- [ ] Micro-segmentation (network policies per service)
- [ ] No implicit trust based on network location
- [ ] Encrypted east-west traffic (mTLS between services)
- [ ] DNS-based service discovery (no hardcoded IPs)

### Device
- [ ] Device posture assessment (MDM compliance)
- [ ] Certificate-based device identity
- [ ] Conditional access policies (managed device required)

### Data
- [ ] Encryption at rest (AES-256)
- [ ] Encryption in transit (TLS 1.3)
- [ ] Data classification (public, internal, confidential, restricted)
- [ ] Data loss prevention (DLP) policies

### Monitoring
- [ ] Log all authentication and authorization events
- [ ] Anomaly detection (unusual access patterns)
- [ ] Continuous compliance monitoring
- [ ] Incident response automation
```

## Defense-in-Depth

### Security Layers

```
Layer 1: PERIMETER
├── WAF (Web Application Firewall)
├── DDoS protection (CloudFlare, AWS Shield)
├── CDN with security headers
└── Rate limiting

Layer 2: NETWORK
├── VPC isolation
├── Security groups / Network policies
├── Private subnets for databases
└── VPN for admin access

Layer 3: APPLICATION
├── Authentication (OAuth2/OIDC)
├── Authorization (RBAC/ABAC)
├── Input validation
├── CSRF/XSS protection
└── Content Security Policy (CSP)

Layer 4: DATA
├── Encryption at rest (AES-256)
├── Encryption in transit (TLS 1.3)
├── Column-level encryption for PII
├── Key management (KMS)
└── Database access control

Layer 5: MONITORING
├── Audit logging
├── SIEM (Security Information and Event Management)
├── Intrusion detection (IDS/IPS)
└── Vulnerability scanning
```

## STRIDE Threat Modeling

### Process

```
1. Identify Assets
   └── What are we protecting? (data, services, reputation)

2. Create Data Flow Diagrams
   └── Map all data flows, trust boundaries, data stores

3. Apply STRIDE per Element
   └── For each element, ask: what could go wrong?

4. Rate and Prioritize
   └── Use DREAD or risk matrix to prioritize threats

5. Define Mitigations
   └── Security controls for each identified threat

6. Validate
   └── Test mitigations, update model as system evolves
```

### STRIDE Analysis Example

```markdown
## Threat Model: User Authentication Flow

### Data Flow
User → [TLS] → Load Balancer → [Internal] → Auth Service → [SQL] → User DB

### STRIDE Analysis

#### Spoofing
| Threat | Target | Impact | Mitigation |
|--------|--------|--------|------------|
| Credential stuffing | Auth Service | Account takeover | Rate limiting, MFA, breached password detection |
| Session hijacking | Load Balancer | Impersonation | Secure cookies (HttpOnly, Secure, SameSite) |
| Token forgery | Auth Service | Unauthorized access | JWT signature verification, short expiry |

#### Tampering
| Threat | Target | Impact | Mitigation |
|--------|--------|--------|------------|
| Man-in-the-middle | TLS connection | Data modification | TLS 1.3, HSTS, certificate pinning |
| SQL injection | User DB | Data corruption | Parameterized queries, ORM, WAF |
| JWT payload modification | Auth token | Privilege escalation | Asymmetric signing (RS256), validation |

#### Repudiation
| Threat | Target | Impact | Mitigation |
|--------|--------|--------|------------|
| Deny login activity | Auth Service | Compliance failure | Audit logging with tamper-proof storage |
| Deny data access | User DB | Accountability gap | Database audit logging, access logs |

#### Information Disclosure
| Threat | Target | Impact | Mitigation |
|--------|--------|--------|------------|
| Password leak | User DB | Mass account compromise | bcrypt/argon2 hashing, salting |
| Error message leakage | Auth Service | System info exposed | Generic error messages, structured logging |
| Token exposure in logs | Application logs | Session theft | Log scrubbing, sensitive field masking |

#### Denial of Service
| Threat | Target | Impact | Mitigation |
|--------|--------|--------|------------|
| Login flood | Auth Service | Service unavailable | Rate limiting, CAPTCHA after N failures |
| Resource exhaustion | Load Balancer | Service degraded | Auto-scaling, connection limits, WAF |

#### Elevation of Privilege
| Threat | Target | Impact | Mitigation |
|--------|--------|--------|------------|
| Broken access control | Auth Service | Admin access gained | RBAC enforcement, principle of least privilege |
| IDOR (Insecure Direct Object Ref) | API endpoints | Access other users' data | Authorization checks on every request |
```

## PASTA Methodology

Process for Attack Simulation and Threat Analysis -- a risk-centric threat modeling approach.

```
Stage 1: Define Objectives
└── Business requirements, compliance needs, risk tolerance

Stage 2: Define Technical Scope
└── Application architecture, technologies, data flows

Stage 3: Decompose Application
└── Trust boundaries, entry/exit points, data stores

Stage 4: Threat Analysis
└── Attack libraries (OWASP, CAPEC), threat intelligence

Stage 5: Vulnerability Analysis
└── Code review, dependency scanning, penetration testing

Stage 6: Attack Modeling
└── Attack trees, kill chains, probable attack scenarios

Stage 7: Risk and Impact Analysis
└── Business impact, likelihood, countermeasure effectiveness
```

## OAuth2/OIDC Flows

### Authorization Code + PKCE (Recommended for All Clients)

```
┌──────┐         ┌──────────┐         ┌────────────┐
│Client│         │ Auth     │         │ Resource   │
│(SPA/ │         │ Server   │         │ Server     │
│Mobile)         │ (IdP)    │         │ (API)      │
└──┬───┘         └────┬─────┘         └─────┬──────┘
   │                   │                     │
   │ 1. Generate code_verifier + code_challenge
   │                   │                     │
   │ 2. Redirect to /authorize              │
   │   ?response_type=code                  │
   │   &client_id=xxx                       │
   │   &redirect_uri=xxx                    │
   │   &scope=openid profile               │
   │   &code_challenge=xxx                  │
   │   &code_challenge_method=S256          │
   │──────────────────→│                     │
   │                   │                     │
   │ 3. User authenticates (login, MFA)     │
   │                   │                     │
   │ 4. Redirect to client with code        │
   │←──────────────────│                     │
   │  ?code=abc123     │                     │
   │                   │                     │
   │ 5. POST /token    │                     │
   │  grant_type=authorization_code         │
   │  code=abc123      │                     │
   │  code_verifier=xxx│                     │
   │──────────────────→│                     │
   │                   │                     │
   │ 6. {access_token, │                     │
   │     id_token,     │                     │
   │     refresh_token}│                     │
   │←──────────────────│                     │
   │                   │                     │
   │ 7. GET /api/resource                   │
   │   Authorization: Bearer {access_token} │
   │────────────────────────────────────────→│
   │                   │                     │
   │ 8. Resource data  │                     │
   │←────────────────────────────────────────│
```

### Client Credentials Flow (Service-to-Service)

```
┌──────────┐         ┌──────────┐         ┌──────────┐
│ Service A│         │ Auth     │         │ Service B│
│ (client) │         │ Server   │         │ (API)    │
└────┬─────┘         └────┬─────┘         └────┬─────┘
     │                     │                    │
     │ 1. POST /token      │                    │
     │  grant_type=client_credentials           │
     │  client_id=xxx      │                    │
     │  client_secret=xxx  │                    │
     │  scope=orders:read  │                    │
     │────────────────────→│                    │
     │                     │                    │
     │ 2. {access_token}   │                    │
     │←────────────────────│                    │
     │                     │                    │
     │ 3. GET /orders      │                    │
     │   Authorization: Bearer {access_token}   │
     │──────────────────────────────────────────→│
     │                     │                    │
     │ 4. Orders data      │                    │
     │←──────────────────────────────────────────│
```

### Token Best Practices

```typescript
// JWT token structure
interface AccessToken {
  // Header
  alg: 'RS256';        // Asymmetric signing (never HS256 for distributed systems)
  typ: 'JWT';
  kid: 'key-2025-03';  // Key ID for rotation

  // Payload
  sub: 'user-123';     // Subject (user ID)
  iss: 'https://auth.example.com';  // Issuer
  aud: 'https://api.example.com';   // Audience
  exp: 1711065600;     // Expiration (15 min)
  iat: 1711064700;     // Issued at
  scope: 'read:orders write:orders'; // Scopes
  roles: ['user'];     // Application roles
}
```

```typescript
// Token validation middleware
import { expressjwt as jwt } from 'express-jwt';
import jwksRsa from 'jwks-rsa';

const validateToken = jwt({
  secret: jwksRsa.expressJwtSecret({
    cache: true,
    rateLimit: true,
    jwksRequestsPerMinute: 5,
    jwksUri: 'https://auth.example.com/.well-known/jwks.json',
  }),
  audience: 'https://api.example.com',
  issuer: 'https://auth.example.com',
  algorithms: ['RS256'],
});

// Scope-based authorization
function requireScope(scope: string) {
  return (req, res, next) => {
    const tokenScopes = req.auth?.scope?.split(' ') || [];
    if (!tokenScopes.includes(scope)) {
      return res.status(403).json({ error: 'Insufficient scope' });
    }
    next();
  };
}

// Usage
app.get('/api/orders', validateToken, requireScope('read:orders'), getOrders);
app.post('/api/orders', validateToken, requireScope('write:orders'), createOrder);
```

## Encryption

### At Rest (AES-256)

```typescript
// Application-level encryption for sensitive fields
import { createCipheriv, createDecipheriv, randomBytes, scryptSync } from 'node:crypto';

const ALGORITHM = 'aes-256-gcm';
const KEY_LENGTH = 32;
const IV_LENGTH = 16;
const TAG_LENGTH = 16;

function encrypt(plaintext: string, key: Buffer): string {
  const iv = randomBytes(IV_LENGTH);
  const cipher = createCipheriv(ALGORITHM, key, iv);

  let encrypted = cipher.update(plaintext, 'utf8', 'hex');
  encrypted += cipher.final('hex');

  const tag = cipher.getAuthTag();

  // IV + Auth Tag + Ciphertext (all hex-encoded)
  return iv.toString('hex') + tag.toString('hex') + encrypted;
}

function decrypt(encryptedHex: string, key: Buffer): string {
  const iv = Buffer.from(encryptedHex.slice(0, IV_LENGTH * 2), 'hex');
  const tag = Buffer.from(encryptedHex.slice(IV_LENGTH * 2, (IV_LENGTH + TAG_LENGTH) * 2), 'hex');
  const encrypted = encryptedHex.slice((IV_LENGTH + TAG_LENGTH) * 2);

  const decipher = createDecipheriv(ALGORITHM, key, iv);
  decipher.setAuthTag(tag);

  let decrypted = decipher.update(encrypted, 'hex', 'utf8');
  decrypted += decipher.final('utf8');
  return decrypted;
}
```

### In Transit (TLS 1.3)

```nginx
# nginx TLS configuration
server {
    listen 443 ssl http2;
    server_name api.example.com;

    ssl_certificate /etc/ssl/certs/api.example.com.pem;
    ssl_certificate_key /etc/ssl/private/api.example.com.key;

    # TLS 1.3 only (or TLS 1.2 minimum)
    ssl_protocols TLSv1.3;
    ssl_prefer_server_ciphers off;

    # HSTS (force HTTPS)
    add_header Strict-Transport-Security "max-age=63072000; includeSubDomains; preload" always;

    # OCSP stapling
    ssl_stapling on;
    ssl_stapling_verify on;
}
```

### Key Management

```markdown
## Key Management Principles

1. **Never hardcode keys** -- use KMS (AWS KMS, GCP Cloud KMS, Azure Key Vault)
2. **Rotate keys regularly** -- automated rotation every 90 days minimum
3. **Envelope encryption** -- encrypt data keys with master keys
4. **Key hierarchy**:
   - Master Key (in HSM/KMS, never exported)
   - Data Encryption Key (DEK, encrypted by master key)
   - Data encrypted with DEK
5. **Separate keys per environment** -- dev keys cannot decrypt prod data
6. **Audit key usage** -- log every key access in CloudTrail/audit logs
```

## API Security

### Security Headers

```typescript
import helmet from 'helmet';

app.use(helmet({
  contentSecurityPolicy: {
    directives: {
      defaultSrc: ["'self'"],
      scriptSrc: ["'self'"],
      styleSrc: ["'self'", "'unsafe-inline'"],
      imgSrc: ["'self'", "data:", "https:"],
      connectSrc: ["'self'", "https://api.example.com"],
      fontSrc: ["'self'"],
      objectSrc: ["'none'"],
      frameAncestors: ["'none'"],
    },
  },
  crossOriginEmbedderPolicy: true,
  crossOriginOpenerPolicy: true,
  crossOriginResourcePolicy: { policy: "same-site" },
  hsts: { maxAge: 63072000, includeSubDomains: true, preload: true },
  referrerPolicy: { policy: "strict-origin-when-cross-origin" },
  xContentTypeOptions: true,  // nosniff
  xFrameOptions: { action: "deny" },
}));
```

### Rate Limiting

```typescript
import rateLimit from 'express-rate-limit';

// Global rate limit
const globalLimiter = rateLimit({
  windowMs: 15 * 60 * 1000, // 15 minutes
  max: 100,                  // 100 requests per window
  standardHeaders: true,     // Return rate limit info in headers
  legacyHeaders: false,
  message: { error: 'Too many requests, please try again later' },
});

// Stricter limit for authentication endpoints
const authLimiter = rateLimit({
  windowMs: 15 * 60 * 1000,
  max: 5,                    // 5 login attempts per 15 minutes
  skipSuccessfulRequests: true,
  message: { error: 'Too many login attempts' },
});

app.use('/api/', globalLimiter);
app.use('/api/auth/login', authLimiter);
```

### Input Validation

```typescript
import { z } from 'zod';

// Define schemas
const createOrderSchema = z.object({
  items: z.array(z.object({
    productId: z.string().uuid(),
    quantity: z.number().int().positive().max(100),
  })).min(1).max(50),
  shippingAddress: z.object({
    street: z.string().min(1).max(200),
    city: z.string().min(1).max(100),
    country: z.string().length(2),  // ISO 3166-1 alpha-2
    postalCode: z.string().regex(/^[a-zA-Z0-9\s-]{3,10}$/),
  }),
  notes: z.string().max(500).optional(),
});

// Validation middleware
function validate(schema: z.ZodSchema) {
  return (req, res, next) => {
    const result = schema.safeParse(req.body);
    if (!result.success) {
      return res.status(400).json({
        error: 'Validation failed',
        details: result.error.flatten().fieldErrors,
      });
    }
    req.body = result.data; // Use parsed (sanitized) data
    next();
  };
}

app.post('/api/orders', validateToken, validate(createOrderSchema), createOrder);
```

## Secrets Management Architecture

```
┌────────────────────────────────────────────────────┐
│                 Secrets Management                  │
│                                                     │
│  ┌─────────────┐    ┌──────────────┐               │
│  │ Developer   │───→│ Vault / AWS  │               │
│  │ (write)     │    │ Secrets Mgr  │               │
│  └─────────────┘    └──────┬───────┘               │
│                            │                        │
│            ┌───────────────┼───────────────┐        │
│            ▼               ▼               ▼        │
│  ┌─────────────┐  ┌──────────────┐ ┌────────────┐ │
│  │ CI/CD       │  │ Kubernetes   │ │ Lambda     │ │
│  │ (build-time)│  │ (runtime)    │ │ (runtime)  │ │
│  │             │  │              │ │            │ │
│  │ Inject as   │  │ External     │ │ Env vars   │ │
│  │ env vars    │  │ Secrets      │ │ from       │ │
│  │ from vault  │  │ Operator     │ │ Secrets Mgr│ │
│  └─────────────┘  └──────────────┘ └────────────┘ │
└────────────────────────────────────────────────────┘

Rules:
- Secrets never in code or Git (even encrypted)
- Secrets never in container images
- Secrets rotated automatically
- Secrets access audited
- Separate secrets per environment
```

### Kubernetes External Secrets Operator

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: api-secrets
  namespace: production
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
  target:
    name: api-secrets
    creationPolicy: Owner
  data:
    - secretKey: DATABASE_URL
      remoteRef:
        key: production/api/database
        property: url
    - secretKey: JWT_SECRET
      remoteRef:
        key: production/api/jwt
        property: signing-key
```

## Compliance Frameworks

### Mapping Requirements to Controls

| Requirement | SOC 2 | GDPR | HIPAA |
|-------------|-------|------|-------|
| Access control | CC6.1 | Art. 32 | 164.312(a)(1) |
| Encryption at rest | CC6.7 | Art. 32 | 164.312(a)(2)(iv) |
| Encryption in transit | CC6.7 | Art. 32 | 164.312(e)(1) |
| Audit logging | CC7.2 | Art. 30 | 164.312(b) |
| Data retention | CC6.5 | Art. 5(1)(e) | 164.530(j) |
| Breach notification | CC7.3 | Art. 33-34 | 164.408 |
| Data minimization | - | Art. 5(1)(c) | 164.502(b) |
| Right to deletion | - | Art. 17 | - |
| Vendor management | CC9.2 | Art. 28 | 164.308(b)(1) |
| Incident response | CC7.4 | Art. 33 | 164.308(a)(6) |

### SOC 2 Trust Service Criteria

| Category | Description |
|----------|-------------|
| Security | Protection against unauthorized access |
| Availability | System is available for operation and use |
| Processing Integrity | System processing is complete and accurate |
| Confidentiality | Information designated as confidential is protected |
| Privacy | Personal information is collected and used appropriately |

## Best Practices

1. **Shift security left** -- integrate security testing in CI/CD, not just before release
2. **Threat model early** -- identify threats during design, not after implementation
3. **Use established standards** -- OAuth2/OIDC for auth, AES-256 for encryption, TLS 1.3 for transport
4. **Automate security scanning** -- SAST, DAST, SCA (dependency scanning) in every pipeline
5. **Principle of least privilege** -- everywhere: IAM, database, API scopes, file system
6. **Defense in depth** -- multiple layers of controls; never rely on a single security measure
7. **Encrypt everything** -- at rest and in transit; no exceptions
8. **Audit everything** -- log authentication, authorization, and data access events
9. **Rotate credentials** -- automated rotation for all secrets, keys, and certificates
10. **Test your incident response** -- run security drills; do not wait for a real breach

## Anti-Patterns

1. **Security through obscurity** -- hiding API endpoints or using non-standard ports is not security
2. **Rolling your own crypto** -- use established libraries (libsodium, node:crypto, AWS KMS)
3. **Symmetric JWT signing in distributed systems** -- use asymmetric (RS256/ES256) for verification without sharing secrets
4. **Storing passwords in plaintext** -- always use bcrypt, scrypt, or argon2
5. **Overly broad IAM permissions** -- `AdministratorAccess` for a Lambda function
6. **Secrets in environment variables without a vault** -- environment variables can leak via logs, crash dumps
7. **No rate limiting** -- APIs without rate limits are DoS targets
8. **Ignoring dependency vulnerabilities** -- `npm audit` / `snyk` findings left unaddressed
9. **Same credentials across environments** -- dev and prod sharing database passwords
10. **Security as an afterthought** -- "We'll add security later" leads to architectural rewrites

## Sources & References

- https://www.nist.gov/publications/zero-trust-architecture -- NIST SP 800-207 Zero Trust Architecture
- https://owasp.org/www-community/Threat_Modeling -- OWASP Threat Modeling
- https://cheatsheetseries.owasp.org/ -- OWASP Cheat Sheet Series
- https://oauth.net/2/ -- OAuth 2.0 specification
- https://openid.net/developers/how-connect-works/ -- OpenID Connect
- https://www.vaultproject.io/docs -- HashiCorp Vault documentation
- https://docs.aws.amazon.com/secretsmanager/ -- AWS Secrets Manager
- https://www.aicpa-cima.com/topic/audit-assurance/audit-and-assurance-greater-than-soc-2 -- SOC 2 overview
- https://gdpr.eu/ -- GDPR compliance guide
- https://external-secrets.io/latest/ -- External Secrets Operator
