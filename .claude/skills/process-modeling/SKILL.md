---
name: process-modeling
description: BPMN 2.0 modeling, as-is/to-be analysis, process optimization, and workflow automation integration
---

# Process Modeling

## Overview

Process modeling is the practice of creating visual representations of business processes to understand, analyze, and improve how work flows through an organization. BPMN (Business Process Model and Notation) 2.0 is the international standard for process modeling.

## BPMN 2.0 Fundamentals

### Flow Objects

The three core element types in BPMN.

#### Events (Things that happen)

```
Start Events (trigger the process):
  ○       None (manual start)
  ○⚡     Message (receives a message)
  ○⏰     Timer (scheduled/delayed start)
  ○⚠️     Signal (broadcast event)

Intermediate Events (occur during the process):
  ◎       None (marks a milestone)
  ◎⚡     Message (wait for/send message)
  ◎⏰     Timer (delay/deadline)
  ◎⚠️     Error (catches/throws error)

End Events (complete the process):
  ●       None (process ends normally)
  ●⚡     Message (sends a message on completion)
  ●⚠️     Error (process ends with error)
  ●✖️     Terminate (kills all parallel paths)
```

#### Activities (Work being performed)

```
┌──────────┐
│   Task   │   Atomic activity (single unit of work)
└──────────┘

┌──────────┐
│ ▸ Task   │   Service Task (automated)
└──────────┘

┌──────────┐
│ 👤 Task  │   User Task (human interaction)
└──────────┘

┌──────────┐
│ ✉️ Task  │   Send/Receive Task (messaging)
└──────────┘

┌─╔════════╗─┐
│ ║Subprocess║│ Subprocess (contains sub-process)
└─╚════════╝─┘
```

#### Gateways (Decision points and flow control)

```
  ◇        Exclusive Gateway (XOR) - one path taken
  ◆        Inclusive Gateway (OR) - one or more paths
  ⊕        Parallel Gateway (AND) - all paths taken
  ⊗        Event-Based Gateway - wait for first event
  ◇+       Complex Gateway - custom condition
```

### BPMN Process Example: Order Fulfillment

```
┌─ Customer ────────────────────────────────────────────┐
│                                                        │
│  ○──→ 👤Place ──→ ◇──→ 👤Enter ──→ 👤Select ──→ ●   │
│       Order       │    Payment     Shipping            │
│                   │                                    │
│                   ▼ (cancel)                           │
│                   ●                                    │
└────────────────────────────────────────────────────────┘

┌─ Warehouse ───────────────────────────────────────────┐
│                                                        │
│  ○⚡──→ ▸Verify ──→ ◇──→ ▸Reserve ──→ 👤Pack ──→ ▸Ship │
│  (order    Stock     │    Stock       Order     Order  │
│  received)           │                           │     │
│                      ▼ (out of stock)            ●⚡   │
│                   ✉️Notify ──→ ●                (shipped│
│                   Customer                   notification)│
└────────────────────────────────────────────────────────┘
```

### Pools and Lanes

Pools represent organizational boundaries (different departments, companies, systems). Lanes subdivide pools by role or responsibility.

```
┌─────────────────────────────────────────────────────┐
│ Order Processing Pool                                │
│                                                      │
│ ┌─ Sales ─────────────────────────────────────────┐ │
│ │ ○──→ 👤Receive ──→ ▸Validate ──→ ◇──→         │ │
│ │       Order         Order         │              │ │
│ └───────────────────────────────────│──────────────┘ │
│                                     │                │
│ ┌─ Finance ─────────────────────────│──────────────┐ │
│ │                                   ▼              │ │
│ │                          👤Process ──→ ▸Generate │ │
│ │                            Payment     Invoice   │ │
│ └──────────────────────────────────────────────────┘ │
│                                                      │
│ ┌─ Warehouse ──────────────────────────────────────┐ │
│ │                          ▸Pick ──→ 👤Ship ──→ ● │ │
│ │                           Items     Order        │ │
│ └──────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────┘
```

### Subprocess Modeling

#### Collapsed Subprocess

```
┌──────────────────┐
│ ▸ Payment Process │  (Details hidden, click to expand)
│      [+]          │
└──────────────────┘
```

#### Expanded Subprocess

```
┌─ Payment Process ─────────────────────────────────┐
│                                                    │
│  ○──→ ▸Validate ──→ ▸Charge ──→ ▸Update ──→ ●   │
│       Card         Card        Ledger             │
│                     │                              │
│                     ▼ (declined)                   │
│                  ✉️Notify ──→ ●⚠️                  │
│                  Customer   (error end)            │
└────────────────────────────────────────────────────┘
```

#### Event Subprocess

```
┌─ Order Process ───────────────────────────────────┐
│                                                    │
│  ○──→ ...normal flow...──→ ●                      │
│                                                    │
│  ┌─ ○⏰ 30-day timeout ──────────────────┐        │
│  │  ▸Cancel ──→ ✉️Notify ──→ ●⚠️         │        │
│  │   Order      Customer                 │        │
│  └────────────────────────────────────────┘        │
└────────────────────────────────────────────────────┘
```

## As-Is vs To-Be Analysis

### As-Is Analysis Process

```markdown
## As-Is Process Documentation

### Step 1: Gather Data
- Interview process participants (all roles)
- Observe the actual process in action
- Collect documents, forms, and system screenshots
- Review existing documentation (if any)

### Step 2: Map Current State
- Create BPMN diagram of current process
- Identify all actors, systems, and handoffs
- Document processing times per step
- Note variations and exceptions

### Step 3: Identify Pain Points

| Step | Pain Point | Impact | Frequency |
|------|-----------|--------|-----------|
| Order entry | Manual data entry from email | 10 min/order, error-prone | 50/day |
| Approval | Manager bottleneck | 2-day average wait | 80% of orders |
| Fulfillment | No inventory check before commit | 15% of orders back-ordered | Daily |
```

### To-Be Design

```markdown
## To-Be Process Design

### Improvement Opportunities

| Current (As-Is) | Future (To-Be) | Benefit |
|-----------------|-----------------|---------|
| Manual email parsing | API integration with ordering system | -10 min/order, -95% errors |
| Manager approval for all orders | Auto-approve orders < $5000 | -1.5 day cycle time |
| Manual inventory check | Real-time inventory reservation | -15% back-orders |
| Paper shipping labels | Automated label generation | -5 min/order |

### Expected Metrics Improvement

| Metric | As-Is | To-Be | Improvement |
|--------|-------|-------|-------------|
| Cycle time (order to ship) | 3.2 days | 0.8 days | 75% reduction |
| Error rate | 8% | 0.5% | 94% reduction |
| Manual effort per order | 25 min | 5 min | 80% reduction |
| Orders processed/day | 50 | 200 | 4x throughput |
```

## Process Optimization

### Bottleneck Identification

```markdown
## Process Analysis

### Step-by-Step Timing

| Step | Processing Time | Wait Time | Total Time | Resource |
|------|----------------|-----------|------------|----------|
| 1. Receive order | 2 min | 0 | 2 min | Sales |
| 2. Validate order | 5 min | 30 min | 35 min | Sales |
| 3. Manager approval | 5 min | 1440 min | 1445 min | Manager |  ← BOTTLENECK
| 4. Check inventory | 10 min | 60 min | 70 min | Warehouse |
| 5. Pick items | 15 min | 30 min | 45 min | Warehouse |
| 6. Pack & ship | 10 min | 0 | 10 min | Shipping |

### Analysis
- Total processing time: 47 minutes
- Total wait time: 1560 minutes (26 hours)
- Process efficiency: 47 / 1607 = 2.9%
- Primary bottleneck: Manager approval (90% of total wait time)
```

### Waste Elimination (Lean)

| Waste Type | Example | Resolution |
|-----------|---------|------------|
| **Overproduction** | Generating reports nobody reads | Eliminate unused reports |
| **Waiting** | Waiting for approval | Auto-approve under threshold |
| **Transport** | Moving data between systems manually | API integration |
| **Over-processing** | 5-step approval for small purchases | Risk-based approval tiers |
| **Inventory** | Backlog of unprocessed requests | WIP limits, flow optimization |
| **Motion** | Switching between 6 tools for one task | Unified interface |
| **Defects** | Data entry errors requiring rework | Validation, automation |

### Value Stream Mapping

```
                Lead Time: 3.2 days
├───────────────────────────────────────────────┤

Order    →  Validate  →  Approve   →  Fulfill  →  Ship
Entry       (5 min)      (5 min)      (25 min)    (10 min)
(2 min)

         30 min      1440 min      90 min      0 min
         wait        wait          wait        wait

Processing Time: 47 min (2.9% of lead time)
Value-Add Time:  42 min (89% of processing time)
```

## Workflow Automation Integration

### n8n (Self-Hosted Automation)

```json
{
  "name": "Order Processing Workflow",
  "nodes": [
    {
      "name": "Webhook Trigger",
      "type": "n8n-nodes-base.webhook",
      "parameters": {
        "path": "new-order",
        "httpMethod": "POST"
      }
    },
    {
      "name": "Validate Order",
      "type": "n8n-nodes-base.function",
      "parameters": {
        "functionCode": "const order = items[0].json;\nif (!order.email || !order.items?.length) {\n  throw new Error('Invalid order');\n}\nreturn items;"
      }
    },
    {
      "name": "Check Inventory",
      "type": "n8n-nodes-base.httpRequest",
      "parameters": {
        "url": "https://api.warehouse.example.com/inventory/check",
        "method": "POST",
        "body": "={{ JSON.stringify($json.items) }}"
      }
    },
    {
      "name": "Send Confirmation",
      "type": "n8n-nodes-base.emailSend",
      "parameters": {
        "toEmail": "={{ $json.email }}",
        "subject": "Order Confirmed #{{ $json.orderId }}",
        "text": "Your order has been confirmed and is being processed."
      }
    }
  ]
}
```

### Temporal (Durable Workflows)

```typescript
// workflows/order-processing.ts
import { proxyActivities, sleep } from '@temporalio/workflow';
import type * as activities from '../activities';

const { validateOrder, reserveInventory, processPayment,
        shipOrder, sendConfirmation, refundPayment } = proxyActivities<typeof activities>({
  startToCloseTimeout: '30s',
  retry: { maximumAttempts: 3 },
});

export async function orderProcessingWorkflow(order: Order): Promise<OrderResult> {
  // Step 1: Validate
  const validatedOrder = await validateOrder(order);

  // Step 2: Reserve inventory (compensatable)
  const reservation = await reserveInventory(validatedOrder);

  try {
    // Step 3: Process payment
    const payment = await processPayment(validatedOrder);

    // Step 4: Wait for approval (if > $5000)
    if (validatedOrder.total > 5000) {
      // Durable timer - survives process restarts
      const approved = await waitForApproval(validatedOrder.id, '48h');
      if (!approved) {
        await refundPayment(payment);
        await releaseInventory(reservation);
        return { status: 'rejected' };
      }
    }

    // Step 5: Ship
    const shipment = await shipOrder(validatedOrder, reservation);

    // Step 6: Notify
    await sendConfirmation(validatedOrder, shipment);

    return { status: 'completed', trackingNumber: shipment.tracking };
  } catch (error) {
    // Saga compensation: undo previous steps
    await refundPayment(payment);
    await releaseInventory(reservation);
    throw error;
  }
}
```

### AWS Step Functions

```json
{
  "Comment": "Order Processing State Machine",
  "StartAt": "ValidateOrder",
  "States": {
    "ValidateOrder": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789:function:validate-order",
      "Next": "CheckInventory",
      "Catch": [{
        "ErrorEquals": ["ValidationError"],
        "Next": "OrderFailed"
      }]
    },
    "CheckInventory": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789:function:check-inventory",
      "Next": "IsInStock"
    },
    "IsInStock": {
      "Type": "Choice",
      "Choices": [{
        "Variable": "$.inStock",
        "BooleanEquals": true,
        "Next": "ProcessPayment"
      }],
      "Default": "NotifyBackorder"
    },
    "ProcessPayment": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789:function:process-payment",
      "Next": "FulfillOrder",
      "Retry": [{
        "ErrorEquals": ["PaymentGatewayTimeout"],
        "IntervalSeconds": 5,
        "MaxAttempts": 3,
        "BackoffRate": 2.0
      }]
    },
    "FulfillOrder": {
      "Type": "Parallel",
      "Branches": [
        {
          "StartAt": "ShipOrder",
          "States": {
            "ShipOrder": {
              "Type": "Task",
              "Resource": "arn:aws:lambda:us-east-1:123456789:function:ship-order",
              "End": true
            }
          }
        },
        {
          "StartAt": "SendConfirmation",
          "States": {
            "SendConfirmation": {
              "Type": "Task",
              "Resource": "arn:aws:lambda:us-east-1:123456789:function:send-confirmation",
              "End": true
            }
          }
        }
      ],
      "Next": "OrderComplete"
    },
    "OrderComplete": {
      "Type": "Succeed"
    },
    "NotifyBackorder": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789:function:notify-backorder",
      "Next": "OrderComplete"
    },
    "OrderFailed": {
      "Type": "Fail",
      "Cause": "Order validation failed"
    }
  }
}
```

## Decision Modeling (DMN)

Decision Model and Notation standardizes how business decisions are defined.

### Decision Table

```
Decision: Determine Shipping Method

| Order Total | Weight (kg) | Customer Tier | Shipping Method |
|-------------|-------------|---------------|-----------------|
| < $25       | < 5         | Standard      | Standard (5-7 days) |
| < $25       | < 5         | Premium       | Express (2-3 days) |
| < $25       | >= 5        | Any           | Freight (7-10 days) |
| >= $25      | < 5         | Standard      | Express (2-3 days) |
| >= $25      | < 5         | Premium       | Next Day |
| >= $25      | >= 5        | Any           | Freight (3-5 days) |
| >= $100     | Any         | Premium       | Next Day Free |
```

### FEEL Expression (DMN)

```
if order.total >= 100 and customer.tier = "Premium"
then "Next Day Free"
else if order.weight >= 5
then "Freight"
else if order.total >= 25 or customer.tier = "Premium"
then "Express"
else "Standard"
```

## Process Metrics

| Metric | Definition | Formula |
|--------|-----------|---------|
| **Cycle Time** | Time from start to end of one instance | End time - Start time |
| **Throughput** | Instances completed per time period | Completed / Time period |
| **Process Efficiency** | Ratio of value-add to total time | Value-add time / Total time |
| **First Pass Yield** | Instances completed without rework | Correct / Total * 100% |
| **Wait Time Ratio** | Time spent waiting vs working | Wait time / Total time |
| **Defect Rate** | Instances requiring correction | Defects / Total * 100% |

## Best Practices

1. **Model the as-is first** -- understand current state before designing future state
2. **Involve process participants** in modeling sessions (not just managers)
3. **Keep diagrams at the right level** -- too much detail obscures; too little hides problems
4. **Validate models** by walking through them with real scenarios
5. **Focus on handoffs** -- most delays and errors occur at handoff points
6. **Measure before optimizing** -- quantify the problem before proposing solutions
7. **Use standard notation** (BPMN) -- ensures models are understood by different audiences
8. **Version your process models** -- processes evolve; track changes
9. **Automate measurement** -- embed process metrics in workflow engines
10. **Start with the happy path**, then add exceptions and error handling

## Anti-Patterns

1. **Modeling for documentation, not improvement** -- models should drive action
2. **Modeling at wrong granularity** -- strategic decisions modeled at task level or vice versa
3. **No as-is baseline** -- designing to-be without understanding current state
4. **Ignoring exceptions** -- the 20% of cases that cause 80% of effort
5. **Technology-first thinking** -- automating a bad process makes it a fast bad process
6. **No stakeholder validation** -- models that do not match reality are useless
7. **One-time modeling** -- processes should be reviewed and updated regularly
8. **Mixing levels of abstraction** -- strategic and operational details in the same diagram

## Sources & References

- https://www.omg.org/spec/BPMN/2.0/ -- BPMN 2.0 Specification (OMG)
- https://www.bpmn.org/ -- BPMN community and resources
- https://camunda.com/bpmn/ -- Camunda BPMN tutorial
- https://docs.temporal.io/ -- Temporal workflow engine documentation
- https://n8n.io/docs/ -- n8n workflow automation
- https://docs.aws.amazon.com/step-functions/ -- AWS Step Functions documentation
- https://www.omg.org/spec/DMN/ -- DMN (Decision Model and Notation) specification
- https://www.lean.org/explore-lean/what-is-lean/ -- Lean principles for process optimization
