---
name: chaos-engineering
description: Chaos engineering principles, fault injection patterns, and resilience validation with Gremlin and Chaos Mesh
---

# Chaos Engineering

## Overview

Chaos engineering is the discipline of experimenting on a system to build confidence in its ability to withstand turbulent conditions in production. Rather than waiting for outages, you proactively inject failures to discover weaknesses before they cause incidents.

## Core Principles

1. **Build a Hypothesis Around Steady State** -- Define measurable normal behavior (e.g., p99 latency < 200ms, error rate < 0.1%)
2. **Vary Real-World Events** -- Inject failures that actually happen: server crashes, network partitions, disk full, dependency timeouts
3. **Run Experiments in Production** -- Test where real traffic and configurations exist (with safety controls)
4. **Automate Experiments to Run Continuously** -- Move beyond one-off tests to continuous validation
5. **Minimize Blast Radius** -- Start small, expand scope only when confidence grows

## The Chaos Experiment Framework

```
1. Define Steady State
   └─ What does "normal" look like? (metrics, SLOs)

2. Hypothesize
   └─ "When X fails, the system will degrade gracefully because of Y"

3. Inject Failure
   └─ Execute the fault injection (network, process, resource)

4. Observe
   └─ Monitor steady-state metrics, dashboards, alerts

5. Learn
   └─ Validate or disprove hypothesis, document findings

6. Fix & Repeat
   └─ Improve resilience, re-run experiment to verify
```

## Fault Injection Patterns

### Network Failures

```yaml
# Chaos Mesh: Network delay
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: network-delay
  namespace: production
spec:
  action: delay
  mode: one
  selector:
    namespaces:
      - production
    labelSelectors:
      app: payment-service
  delay:
    latency: "500ms"
    jitter: "100ms"
    correlation: "50"
  duration: "5m"
  scheduler:
    cron: "@every 24h"
```

```yaml
# Network partition between services
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: partition-payment-from-db
spec:
  action: partition
  mode: all
  selector:
    labelSelectors:
      app: payment-service
  direction: both
  target:
    mode: all
    selector:
      labelSelectors:
        app: postgres
  duration: "2m"
```

### CPU and Memory Stress

```yaml
# Chaos Mesh: CPU stress
apiVersion: chaos-mesh.org/v1alpha1
kind: StressChaos
metadata:
  name: cpu-stress
spec:
  mode: one
  selector:
    labelSelectors:
      app: api-gateway
  stressors:
    cpu:
      workers: 4
      load: 80   # 80% CPU usage
  duration: "10m"
```

```yaml
# Memory stress
apiVersion: chaos-mesh.org/v1alpha1
kind: StressChaos
metadata:
  name: memory-stress
spec:
  mode: one
  selector:
    labelSelectors:
      app: api-gateway
  stressors:
    memory:
      workers: 2
      size: "512MB"
  duration: "5m"
```

### Process Kill (Pod Failure)

```yaml
# Kill a pod
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: pod-kill
spec:
  action: pod-kill
  mode: one
  selector:
    namespaces:
      - production
    labelSelectors:
      app: order-service
  duration: "30s"
  scheduler:
    cron: "@every 1h"
```

### Disk Fill

```yaml
# Chaos Mesh: Disk fill
apiVersion: chaos-mesh.org/v1alpha1
kind: IOChaos
metadata:
  name: disk-fill
spec:
  action: fault
  mode: one
  selector:
    labelSelectors:
      app: logging-service
  volumePath: /var/log
  errno: 28  # ENOSPC (no space left on device)
  duration: "5m"
```

## Gremlin (SaaS Chaos Platform)

### Attack Types

| Category | Attack | What It Does |
|----------|--------|-------------|
| Resource | CPU | Consumes CPU cycles |
| Resource | Memory | Allocates memory |
| Resource | Disk | Fills disk space |
| Resource | IO | Generates I/O load |
| Network | Latency | Adds delay to network packets |
| Network | Packet Loss | Drops network packets |
| Network | DNS | Blocks DNS resolution |
| Network | Blackhole | Drops all network traffic |
| State | Process Kill | Kills specified processes |
| State | Shutdown | Shuts down the host |
| State | Time Travel | Skews system clock |

### Gremlin CLI Examples

```bash
# Install Gremlin agent
gremlin init -s YOUR_TEAM_ID --secret YOUR_SECRET

# CPU attack
gremlin attack cpu --length 300 --cores 2 --percent 80

# Network latency
gremlin attack latency --length 300 --delay 500 --jitter 100

# DNS blackhole
gremlin attack dns --length 300 --domains "payment-api.example.com"

# Process kill with interval
gremlin attack process --length 600 --process "node" --interval 60
```

### Gameday Planning with Gremlin

```markdown
# Gameday: Payment Service Resilience

## Date: 2025-03-15
## Participants: SRE Team, Payment Team, Platform Team

## Pre-Gameday Checklist
- [ ] All participants have Gremlin dashboard access
- [ ] Monitoring dashboards shared (Grafana, Datadog)
- [ ] Rollback procedures documented
- [ ] Communication channel set up (#gameday-march Slack)
- [ ] Stakeholders notified (Support, Product)
- [ ] War room booked (or Zoom link)

## Experiments

### Experiment 1: Payment DB Latency
**Hypothesis**: Payment service handles 500ms DB latency with <5% error increase
**Attack**: Network latency on DB connection (500ms, 5min)
**Observe**: Payment success rate, p95 latency, retry counts
**Abort Criteria**: Error rate >10% or p99 >5s

### Experiment 2: Cache Failure
**Hypothesis**: Redis failure degrades to DB reads without user-facing errors
**Attack**: Kill Redis process
**Observe**: Response time, DB connection pool, cache miss rate
**Abort Criteria**: Homepage error rate >1%

### Experiment 3: Payment Provider Timeout
**Hypothesis**: Stripe timeout triggers fallback to queued processing
**Attack**: Blackhole traffic to Stripe API
**Observe**: Order completion rate, queue depth, user error messages
**Abort Criteria**: Zero orders completing for >2min

## Post-Gameday
- [ ] Document findings per experiment
- [ ] Create action items for failures
- [ ] Schedule follow-up gameday in 4 weeks
- [ ] Share summary with engineering org
```

## Chaos Mesh (Kubernetes-Native)

### Installation

```bash
# Install via Helm
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace=chaos-mesh \
  --create-namespace \
  --set chaosDaemon.runtime=containerd \
  --set chaosDaemon.socketPath=/run/containerd/containerd.sock
```

### Workflow (Multi-Step Experiments)

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: Workflow
metadata:
  name: resilience-test
spec:
  entry: the-entry
  templates:
    - name: the-entry
      templateType: Serial
      deadline: "30m"
      children:
        - network-delay
        - pod-kill
        - verify-recovery

    - name: network-delay
      templateType: NetworkChaos
      deadline: "5m"
      networkChaos:
        action: delay
        mode: one
        selector:
          labelSelectors:
            app: api
        delay:
          latency: "300ms"

    - name: pod-kill
      templateType: PodChaos
      deadline: "2m"
      podChaos:
        action: pod-kill
        mode: one
        selector:
          labelSelectors:
            app: api

    - name: verify-recovery
      templateType: Suspend
      deadline: "5m"  # Wait for recovery
```

## Litmus Chaos (CNCF Project)

```yaml
# ChaosEngine resource
apiVersion: litmuschaos.io/v1alpha1
kind: ChaosEngine
metadata:
  name: payment-chaos
  namespace: production
spec:
  appinfo:
    appns: production
    applabel: app=payment-service
    appkind: deployment
  engineState: active
  chaosServiceAccount: litmus-admin
  experiments:
    - name: pod-delete
      spec:
        components:
          env:
            - name: TOTAL_CHAOS_DURATION
              value: "60"
            - name: CHAOS_INTERVAL
              value: "10"
            - name: FORCE
              value: "false"
        probe:
          - name: check-payment-health
            type: httpProbe
            httpProbe/inputs:
              url: http://payment-service:8080/health
              method:
                get:
                  criteria: ==
                  responseCode: "200"
            mode: Continuous
            runProperties:
              probeTimeout: 5
              interval: 2
              retry: 3
```

## AWS Fault Injection Service (FIS)

```json
{
  "description": "EC2 instance stop experiment",
  "targets": {
    "ec2-instances": {
      "resourceType": "aws:ec2:instance",
      "resourceTags": {
        "Environment": "staging",
        "Service": "api"
      },
      "selectionMode": "COUNT(1)"
    }
  },
  "actions": {
    "stop-instance": {
      "actionId": "aws:ec2:stop-instances",
      "parameters": {
        "startInstancesAfterDuration": "PT5M"
      },
      "targets": {
        "Instances": "ec2-instances"
      }
    }
  },
  "stopConditions": [
    {
      "source": "aws:cloudwatch:alarm",
      "value": "arn:aws:cloudwatch:us-east-1:123456789:alarm:HighErrorRate"
    }
  ],
  "roleArn": "arn:aws:iam::123456789:role/FISRole"
}
```

## Resilience Validation

### Key Metrics to Monitor During Experiments

| Metric | Why It Matters |
|--------|---------------|
| Error rate (4xx, 5xx) | Direct user impact |
| Latency (p50, p95, p99) | Degradation detection |
| Throughput (RPS) | Capacity impact |
| Circuit breaker state | Failover working? |
| Retry count | Cascading failure risk |
| Queue depth | Backpressure signal |
| Pod restarts | Recovery behavior |
| Resource utilization | Saturation detection |

### Steady-State Validation Script

```javascript
// k6 script to validate steady state during chaos
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate } from 'k6/metrics';

const errorRate = new Rate('errors');

export const options = {
  stages: [
    { duration: '2m', target: 20 },  // Normal load
    { duration: '10m', target: 20 }, // Sustain during chaos window
    { duration: '2m', target: 0 },   // Cool down
  ],
  thresholds: {
    errors: ['rate<0.05'],               // <5% errors during chaos
    http_req_duration: ['p(95)<1000'],   // p95 <1s during chaos
  },
};

export default function () {
  const res = http.get('http://api.example.com/health');
  const failed = !check(res, {
    'status is 200': (r) => r.status === 200,
    'response time OK': (r) => r.timings.duration < 1000,
  });
  errorRate.add(failed);
  sleep(1);
}
```

## MTTR Reduction Strategies

1. **Automated detection** -- Alerts fire within 1 minute of anomaly
2. **Clear runbooks** -- Step-by-step procedures for known failure modes
3. **Pre-validated rollback** -- One-click rollback tested in chaos experiments
4. **Circuit breakers** -- Automatic failover without human intervention
5. **Feature flags** -- Kill switch for problematic features
6. **Blue-green deployments** -- Instant traffic shift to known-good version

## Blast Radius Limitation

### Progressive Complexity Model

```
Level 1: Development Environment
  └─ Start here. No real users affected.

Level 2: Staging/Pre-production
  └─ Production-like but isolated.

Level 3: Canary (1-5% of production traffic)
  └─ Real users, minimal impact.

Level 4: Single AZ / Region
  └─ Production, limited scope.

Level 5: Full Production
  └─ Confident in resilience.
```

### Safety Controls

- **Abort conditions** -- Automated halt when metrics breach thresholds
- **Time limits** -- Every experiment has a maximum duration
- **Scope limits** -- Target specific services, not entire infrastructure
- **Manual override** -- One-click emergency stop
- **Notification** -- Team is aware an experiment is running
- **Business hours only** -- Run during peak staffing (initially)

## Disaster Recovery Testing

### DR Drill Checklist

```markdown
## Pre-Drill
- [ ] Notify stakeholders and support team
- [ ] Verify backup integrity
- [ ] Document expected RTO (Recovery Time Objective)
- [ ] Document expected RPO (Recovery Point Objective)
- [ ] Prepare monitoring dashboard

## During Drill
- [ ] Simulate primary region failure
- [ ] Measure DNS failover time
- [ ] Validate read replicas promotion
- [ ] Test data consistency after failover
- [ ] Verify application functionality in DR region
- [ ] Measure actual RTO (time to recovery)
- [ ] Measure actual RPO (data loss window)

## Post-Drill
- [ ] Compare actual vs expected RTO/RPO
- [ ] Document gaps and surprises
- [ ] Create action items for improvements
- [ ] Update runbooks based on learnings
- [ ] Schedule next drill (quarterly recommended)
```

## Best Practices

1. **Start in non-production** -- Build confidence before touching production
2. **Define abort criteria upfront** -- Know exactly when to stop
3. **Automate experiments** -- Manual chaos is inconsistent and error-prone
4. **Run during business hours** initially -- when your team is available to respond
5. **Communicate broadly** -- Everyone should know a chaos experiment is running
6. **Start with known weaknesses** -- Test your hypotheses about existing risks first
7. **Measure MTTR** -- The goal is faster detection and recovery, not zero failures
8. **Make it continuous** -- Schedule regular experiments, not just one-off gamedays
9. **Fix findings promptly** -- Chaos experiments without follow-up are waste
10. **Celebrate learning** -- Failed hypotheses are valuable, not embarrassing

## Anti-Patterns

1. **Chaos without observability** -- You cannot learn from failures you cannot see
2. **No abort mechanism** -- Every experiment must have an emergency stop
3. **Starting in production** without staging validation first
4. **Testing only infrastructure** -- Application-level chaos (bad data, slow dependencies) matters too
5. **Running experiments during incidents** -- Do not add chaos to existing problems
6. **No hypothesis** -- Random failure injection without measurement is just breaking things
7. **Blaming individuals** -- Chaos engineering reveals system weaknesses, not people failures
8. **Infrequent testing** -- Annual gamedays are not enough; resilience regresses

## Sources & References

- https://principlesofchaos.org/ -- Principles of Chaos Engineering
- https://www.gremlin.com/community/tutorials/ -- Gremlin tutorials and guides
- https://chaos-mesh.org/docs/ -- Chaos Mesh documentation
- https://litmuschaos.io/docs/ -- Litmus Chaos documentation
- https://docs.aws.amazon.com/fis/ -- AWS Fault Injection Service docs
- https://netflix.github.io/chaosmonkey/ -- Netflix Chaos Monkey
- https://www.gremlin.com/gameday/ -- Gameday planning guide
- https://sre.google/sre-book/testing-reliability/ -- Google SRE: Testing Reliability
