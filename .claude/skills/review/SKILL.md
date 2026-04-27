---
name: review
description: Review recent code changes for quality, security, and correctness
invocation: /review
---

# Code Review

Review recent code changes for quality, security, and correctness.

## Usage

```
/review              # Review uncommitted changes
/review HEAD~3       # Review last 3 commits
/review feature-branch  # Review branch changes vs main
```

## Review Checklist

### Code Quality
- [ ] Code follows project conventions and style
- [ ] Functions are focused and appropriately sized
- [ ] Variable and function names are clear
- [ ] No duplicated code that should be extracted
- [ ] Error handling is appropriate
- [ ] No TODO/FIXME without tracking issues

### Security
- [ ] No hardcoded secrets or credentials
- [ ] User input is validated and sanitized
- [ ] SQL/NoSQL injection prevention
- [ ] XSS prevention in frontend code
- [ ] Authentication/authorization checks present
- [ ] Sensitive data not logged

### Correctness
- [ ] Logic handles edge cases (null, empty, boundary)
- [ ] Async operations properly awaited
- [ ] Resources properly cleaned up (connections, files)
- [ ] No race conditions in concurrent code
- [ ] Error messages are helpful

### Testing
- [ ] New functionality has tests
- [ ] Tests cover happy path and error cases
- [ ] Tests are deterministic (no flaky tests)
- [ ] Test names clearly describe what they test

## Output Format

```markdown
## Code Review Summary

### Overall: ✅ Approve / ⚠️ Request Changes / ❌ Block

### Findings

#### 🔴 Critical
- [Finding with file:line reference]

#### 🟡 Suggestions
- [Improvement suggestion with rationale]

#### 🟢 Positive
- [What was done well]
```
