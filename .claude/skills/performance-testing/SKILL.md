---
name: performance-testing
description: Performance and load testing with k6 including test patterns, CI integration, and monitoring
---

# Performance Testing

## Overview

Performance testing validates that your system meets non-functional requirements under load. k6 is a modern, developer-friendly load testing tool by Grafana Labs that uses JavaScript for test scripts and provides excellent CI/CD integration.

## Installation

```bash
# macOS
brew install k6

# Docker
docker run --rm -i grafana/k6 run - < script.js

# Linux
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
  --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D68
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | \
  sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update && sudo apt-get install k6
```

## Test Types

### Load Test (Normal traffic simulation)

```javascript
// load-test.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '2m', target: 50 },   // Ramp up to 50 users
    { duration: '5m', target: 50 },   // Stay at 50 users
    { duration: '2m', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],  // 95% of requests < 500ms
    http_req_failed: ['rate<0.01'],    // Error rate < 1%
    http_reqs: ['rate>100'],           // Throughput > 100 RPS
  },
};

export default function () {
  const res = http.get('http://localhost:3000/api/products');

  check(res, {
    'status is 200': (r) => r.status === 200,
    'response time < 500ms': (r) => r.timings.duration < 500,
    'body has products': (r) => JSON.parse(r.body).data.length > 0,
  });

  sleep(1); // Think time between requests
}
```

### Stress Test (Find breaking point)

```javascript
// stress-test.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '2m', target: 100 },  // Ramp up to normal load
    { duration: '5m', target: 100 },  // Stay at normal
    { duration: '2m', target: 200 },  // Push beyond normal
    { duration: '5m', target: 200 },  // Stay at stress level
    { duration: '2m', target: 300 },  // Push to breaking point
    { duration: '5m', target: 300 },  // Stay at breaking point
    { duration: '5m', target: 0 },    // Ramp down (recovery)
  ],
  thresholds: {
    http_req_duration: ['p(99)<2000'],
    http_req_failed: ['rate<0.05'],    // Allow 5% errors under stress
  },
};

export default function () {
  const res = http.get('http://localhost:3000/api/products');
  check(res, {
    'status is not 500': (r) => r.status !== 500,
  });
  sleep(0.5);
}
```

### Spike Test (Sudden traffic surge)

```javascript
// spike-test.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '10s', target: 10 },    // Warm up
    { duration: '1m', target: 10 },     // Normal traffic
    { duration: '10s', target: 500 },   // SPIKE!
    { duration: '3m', target: 500 },    // Stay at spike
    { duration: '10s', target: 10 },    // Spike drops
    { duration: '3m', target: 10 },     // Recovery period
    { duration: '10s', target: 0 },     // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<3000'],  // More lenient under spike
    http_req_failed: ['rate<0.10'],     // Allow 10% errors during spike
  },
};

export default function () {
  const res = http.get('http://localhost:3000/api/products');
  check(res, {
    'still responding': (r) => r.status !== 0,
    'not server error': (r) => r.status < 500,
  });
  sleep(0.3);
}
```

### Soak Test (Extended duration, detect memory leaks)

```javascript
// soak-test.js
import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '5m', target: 50 },    // Ramp up
    { duration: '4h', target: 50 },    // Stay at moderate load for 4 hours
    { duration: '5m', target: 0 },     // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'],
    http_req_failed: ['rate<0.01'],
  },
};

export default function () {
  const res = http.get('http://localhost:3000/api/products');
  check(res, {
    'status is 200': (r) => r.status === 200,
    'no degradation': (r) => r.timings.duration < 500,
  });
  sleep(2);
}
```

## Realistic User Simulation (Scenarios)

```javascript
// realistic-simulation.js
import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { SharedArray } from 'k6/data';

// Load test data once, share across VUs
const users = new SharedArray('users', function () {
  return JSON.parse(open('./testdata/users.json'));
});

export const options = {
  scenarios: {
    // 70% of traffic: browsing
    browsing: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 70 },
        { duration: '5m', target: 70 },
        { duration: '2m', target: 0 },
      ],
      exec: 'browsingFlow',
    },
    // 20% of traffic: searching
    searching: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 20 },
        { duration: '5m', target: 20 },
        { duration: '2m', target: 0 },
      ],
      exec: 'searchFlow',
    },
    // 10% of traffic: purchasing
    purchasing: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '2m', target: 10 },
        { duration: '5m', target: 10 },
        { duration: '2m', target: 0 },
      ],
      exec: 'purchaseFlow',
    },
  },
  thresholds: {
    'http_req_duration{scenario:browsing}': ['p(95)<300'],
    'http_req_duration{scenario:searching}': ['p(95)<500'],
    'http_req_duration{scenario:purchasing}': ['p(95)<1000'],
    http_req_failed: ['rate<0.01'],
  },
};

export function browsingFlow() {
  group('Browse Products', () => {
    const res = http.get('http://localhost:3000/api/products?page=1');
    check(res, { 'products loaded': (r) => r.status === 200 });
    sleep(Math.random() * 3 + 1); // 1-4s think time

    const products = JSON.parse(res.body).data;
    if (products.length > 0) {
      const product = products[Math.floor(Math.random() * products.length)];
      const detail = http.get(`http://localhost:3000/api/products/${product.id}`);
      check(detail, { 'product detail loaded': (r) => r.status === 200 });
      sleep(Math.random() * 5 + 2); // 2-7s reading product
    }
  });
}

export function searchFlow() {
  group('Search Products', () => {
    const queries = ['laptop', 'phone', 'headphones', 'keyboard', 'monitor'];
    const query = queries[Math.floor(Math.random() * queries.length)];

    const res = http.get(`http://localhost:3000/api/products?search=${query}`);
    check(res, { 'search returned results': (r) => r.status === 200 });
    sleep(Math.random() * 2 + 1);
  });
}

export function purchaseFlow() {
  group('Purchase Flow', () => {
    const user = users[Math.floor(Math.random() * users.length)];

    // Login
    const loginRes = http.post('http://localhost:3000/api/auth/login', JSON.stringify({
      email: user.email,
      password: user.password,
    }), { headers: { 'Content-Type': 'application/json' } });

    check(loginRes, { 'logged in': (r) => r.status === 200 });

    if (loginRes.status === 200) {
      const token = JSON.parse(loginRes.body).token;
      const headers = {
        Authorization: `Bearer ${token}`,
        'Content-Type': 'application/json',
      };

      // Add to cart
      sleep(Math.random() * 2 + 1);
      const cartRes = http.post('http://localhost:3000/api/cart', JSON.stringify({
        productId: 'prod-001',
        quantity: 1,
      }), { headers });
      check(cartRes, { 'added to cart': (r) => r.status === 201 });

      // Checkout
      sleep(Math.random() * 3 + 2);
      const checkoutRes = http.post('http://localhost:3000/api/orders', JSON.stringify({
        paymentMethod: 'card',
      }), { headers });
      check(checkoutRes, { 'order placed': (r) => r.status === 201 });
    }
  });
}
```

## Custom Metrics

```javascript
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Gauge, Rate, Trend } from 'k6/metrics';

// Custom metrics
const orderSuccessRate = new Rate('order_success_rate');
const orderDuration = new Trend('order_duration', true);
const activeOrders = new Gauge('active_orders');
const totalOrders = new Counter('total_orders');

export const options = {
  thresholds: {
    order_success_rate: ['rate>0.95'],     // 95% success rate
    order_duration: ['p(95)<2000'],        // 95th percentile < 2s
  },
};

export default function () {
  const start = Date.now();

  const res = http.post('http://localhost:3000/api/orders', JSON.stringify({
    items: [{ id: 'prod-001', qty: 1 }],
  }), { headers: { 'Content-Type': 'application/json' } });

  const duration = Date.now() - start;
  const success = res.status === 201;

  orderSuccessRate.add(success);
  orderDuration.add(duration);
  totalOrders.add(1);

  if (success) {
    activeOrders.add(1);
  }
}
```

## Threshold Definitions & SLA Validation

```javascript
export const options = {
  thresholds: {
    // Response time thresholds
    http_req_duration: [
      'p(50)<200',     // Median < 200ms
      'p(90)<400',     // 90th percentile < 400ms
      'p(95)<500',     // 95th percentile < 500ms
      'p(99)<1000',    // 99th percentile < 1s
      'max<3000',      // No request > 3s
    ],

    // Error rate
    http_req_failed: ['rate<0.01'],   // < 1% failure rate

    // Throughput
    http_reqs: ['rate>50'],           // > 50 requests/second

    // Custom metric thresholds
    'http_req_duration{name:login}': ['p(95)<1000'],
    'http_req_duration{name:api}': ['p(95)<300'],

    // Abort test if threshold breached early
    http_req_failed: [{
      threshold: 'rate<0.1',
      abortOnFail: true,
      delayAbortEval: '30s',
    }],
  },
};
```

## CI Integration (GitHub Actions)

```yaml
name: Performance Tests
on:
  pull_request:
    branches: [main]
  schedule:
    - cron: '0 6 * * 1'  # Weekly on Monday 6am

jobs:
  performance:
    runs-on: ubuntu-latest
    timeout-minutes: 30

    services:
      app:
        image: myapp:latest
        ports:
          - 3000:3000

    steps:
      - uses: actions/checkout@v4

      - name: Install k6
        run: |
          sudo gpg -k
          sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg \
            --keyserver hkp://keyserver.ubuntu.com:80 \
            --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D68
          echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | \
            sudo tee /etc/apt/sources.list.d/k6.list
          sudo apt-get update && sudo apt-get install k6

      - name: Run load test
        run: k6 run --out json=results.json perf/load-test.js

      - name: Upload results
        uses: actions/upload-artifact@v4
        if: always()
        with:
          name: k6-results
          path: results.json

      - name: Comment PR with results
        if: github.event_name == 'pull_request'
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const results = fs.readFileSync('results.json', 'utf8');
            // Parse and format results for PR comment
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `## Performance Test Results\n\`\`\`\n${results.slice(0, 3000)}\n\`\`\``
            });
```

## Performance Regression Detection

```javascript
// Compare against baseline
import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';

const apiLatency = new Trend('api_latency', true);

// Baseline from previous run (stored in CI artifact or database)
const BASELINE = {
  p95: 450,  // ms
  p99: 800,  // ms
};

export const options = {
  thresholds: {
    // Fail if >10% regression from baseline
    api_latency: [
      `p(95)<${BASELINE.p95 * 1.1}`,
      `p(99)<${BASELINE.p99 * 1.1}`,
    ],
  },
  stages: [
    { duration: '1m', target: 50 },
    { duration: '3m', target: 50 },
    { duration: '1m', target: 0 },
  ],
};

export default function () {
  const res = http.get('http://localhost:3000/api/products');
  apiLatency.add(res.timings.duration);
  check(res, { 'status 200': (r) => r.status === 200 });
}
```

## InfluxDB + Grafana Dashboards

```bash
# Send k6 results to InfluxDB
k6 run --out influxdb=http://localhost:8086/k6 load-test.js

# With authentication
k6 run --out influxdb=http://user:pass@localhost:8086/k6 load-test.js
```

### Docker Compose for k6 + InfluxDB + Grafana

```yaml
# docker-compose.yml
services:
  influxdb:
    image: influxdb:1.8
    ports:
      - "8086:8086"
    environment:
      - INFLUXDB_DB=k6

  grafana:
    image: grafana/grafana:latest
    ports:
      - "3001:3000"
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
      - GF_AUTH_ANONYMOUS_ORG_ROLE=Admin
    volumes:
      - ./grafana/dashboards:/var/lib/grafana/dashboards
      - ./grafana/provisioning:/etc/grafana/provisioning
    depends_on:
      - influxdb

  k6:
    image: grafana/k6:latest
    volumes:
      - ./perf:/scripts
    command: run --out influxdb=http://influxdb:8086/k6 /scripts/load-test.js
    depends_on:
      - influxdb
```

## Browser-Based k6 Testing

```javascript
// browser-test.js
import { browser } from 'k6/browser';
import { check } from 'k6';

export const options = {
  scenarios: {
    ui: {
      executor: 'shared-iterations',
      options: {
        browser: {
          type: 'chromium',
        },
      },
    },
  },
  thresholds: {
    browser_web_vital_lcp: ['p(95)<2500'],  // Largest Contentful Paint
    browser_web_vital_fid: ['p(95)<100'],   // First Input Delay
    browser_web_vital_cls: ['p(95)<0.1'],   // Cumulative Layout Shift
  },
};

export default async function () {
  const page = await browser.newPage();

  try {
    await page.goto('http://localhost:3000/');
    await page.waitForSelector('[data-testid="product-list"]');

    const heading = await page.locator('h1').textContent();
    check(heading, {
      'page title correct': (h) => h === 'Products',
    });

    await page.locator('button[name="add-to-cart"]').first().click();
    await page.waitForSelector('[data-testid="cart-count"]');
  } finally {
    await page.close();
  }
}
```

## Comparison with Other Tools

| Feature | k6 | Gatling | Locust | JMeter |
|---------|-----|---------|--------|--------|
| Language | JavaScript | Scala/Java | Python | XML/GUI |
| Protocol | HTTP, WS, gRPC | HTTP, WS | HTTP | HTTP, JDBC, LDAP |
| Browser testing | Yes (chromium) | No | No | Yes (Selenium) |
| Cloud offering | Grafana Cloud k6 | Gatling Enterprise | Locust Cloud | BlazeMeter |
| CI integration | Excellent | Good | Good | Poor |
| Scripting UX | Modern JS | DSL | Python | GUI-heavy |
| Resource usage | Low (Go binary) | Medium (JVM) | Low (Python) | High (JVM) |
| Distributed | Yes (native) | Yes | Yes (built-in) | Yes (complex) |

## Best Practices

1. **Start with baseline** -- run load tests against your current production-like environment first
2. **Use realistic think time** -- `sleep(Math.random() * 3 + 1)` simulates real user pausing
3. **Test with production-like data** -- use anonymized production data volumes
4. **Run regularly** -- schedule weekly performance tests, not just before releases
5. **Set meaningful thresholds** -- derive from SLAs, not arbitrary numbers
6. **Test in isolation** -- dedicated environment to avoid noisy neighbors
7. **Monitor the system under test** -- correlate k6 results with server metrics (CPU, memory, DB)
8. **Version your test scripts** -- commit k6 scripts alongside application code
9. **Use scenarios** -- model real traffic patterns with multiple user behaviors
10. **Automate regression detection** -- compare against baseline in CI pipeline

## Anti-Patterns

1. **Testing in shared environments** -- other workloads skew results
2. **No think time** -- unrealistic load that hammers the server
3. **Testing only happy paths** -- include error scenarios and edge cases
4. **Ignoring ramp-up** -- sudden max load does not reflect real traffic
5. **Running from the same machine** -- saturating the test client gives false results
6. **No baseline** -- you cannot detect regression without a reference point
7. **Testing only throughput** -- latency percentiles matter more than averages
8. **Using averages** -- always use percentiles (p50, p95, p99); averages hide outliers

## Sources & References

- https://k6.io/docs/ -- k6 official documentation
- https://k6.io/docs/testing-guides/test-types/ -- k6 test types guide
- https://k6.io/docs/using-k6/scenarios/ -- Scenarios documentation
- https://k6.io/docs/using-k6/thresholds/ -- Thresholds reference
- https://grafana.com/docs/k6/latest/results-output/real-time/influxdb/ -- InfluxDB output
- https://k6.io/docs/using-k6-browser/overview/ -- Browser testing with k6
- https://grafana.com/blog/2022/03/22/a-beginners-guide-to-load-testing-with-grafana-k6/ -- k6 beginner guide
- https://github.com/grafana/k6 -- k6 GitHub repository
