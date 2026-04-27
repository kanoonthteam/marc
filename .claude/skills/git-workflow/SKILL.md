---
name: git-workflow
description: Git branching strategies, merge vs rebase, conventional commits, monorepo patterns, and recovery techniques
---

# Git Workflow

## Overview

A well-defined Git workflow improves collaboration, reduces merge conflicts, and enables reliable releases. This skill covers branching strategies, merge vs rebase decisions, branch protection, monorepo patterns, commit conventions, and recovery techniques.

## Branching Strategies Comparison

| Aspect | Git Flow | GitHub Flow | Trunk-Based Development |
|--------|----------|-------------|------------------------|
| Main branches | `main` + `develop` | `main` only | `main` only |
| Feature branches | Long-lived | Short-lived (<1 day ideal) | Very short-lived or none |
| Release branches | Yes | No | No (use release tags) |
| Hotfix branches | Yes | No (fix on main) | No (fix on main) |
| Complexity | High | Low | Lowest |
| Release cadence | Scheduled releases | Continuous | Continuous |
| Best for | Versioned products (mobile, desktop) | Web apps, SaaS | High-performing teams, CI/CD |
| Feature flags | Optional | Optional | Required |
| Team size | Any | Any | Requires mature CI/CD |

### Git Flow

```
main ─────●─────────────────────────────●───────── (releases only)
           \                           /
develop ────●───●───●───●───●───●───●───●───●───── (integration)
             \     / \       / \     /
feature/      ●───●   ●───●   ●───●               (feature work)
login               \     /
                 release/  (release prep)
                 v2.0
                     \
                  hotfix/  ●──●                    (production fixes)
                  fix-login
```

**When to use**: Mobile apps, desktop software, anything with versioned releases where multiple versions are maintained simultaneously.

### GitHub Flow

```
main ─────●───●───●───●───●───●───●───●───●─────── (always deployable)
           \     / \     / \     /
feature/    ●───●   ●───●   ●───●                  (PR per feature)
add-search    |       |       |
              PR      PR      PR
```

**When to use**: Web applications, SaaS, APIs -- any project with continuous deployment.

### Trunk-Based Development

```
main ──●──●──●──●──●──●──●──●──●──●──●──●──●─────── (always releasable)
        \/ \/ \/
     (very short branches, < 1 day)
     or direct commits to main

Feature flags control visibility:
  main has code for Feature X, but behind a flag
  Flag ON  → Feature visible to users
  Flag OFF → Feature hidden, code still deployed
```

**When to use**: Teams with strong CI/CD, comprehensive test suites, and feature flag infrastructure. Google, Facebook, and Netflix use this approach.

## Feature Branching

### Naming Conventions

```bash
# Pattern: type/ticket-description
feature/PROJ-123-user-authentication
bugfix/PROJ-456-fix-login-redirect
hotfix/PROJ-789-security-patch
chore/PROJ-101-update-dependencies
docs/PROJ-202-api-documentation
refactor/PROJ-303-extract-auth-service
```

### Feature Branch Lifecycle

```bash
# 1. Create branch from main (or develop for Git Flow)
git checkout main
git pull origin main
git checkout -b feature/PROJ-123-user-auth

# 2. Work on the feature (small, frequent commits)
git add src/auth/login.ts
git commit -m "feat(auth): add login endpoint"

git add src/auth/middleware.ts
git commit -m "feat(auth): add JWT validation middleware"

git add tests/auth.test.ts
git commit -m "test(auth): add login and middleware tests"

# 3. Keep branch up to date
git fetch origin
git rebase origin/main  # or merge, depending on team preference

# 4. Push and create PR
git push -u origin feature/PROJ-123-user-auth
gh pr create --title "feat: add user authentication" --body "..."

# 5. After PR approval and merge
git checkout main
git pull origin main
git branch -d feature/PROJ-123-user-auth
```

## Merge vs Rebase

### When to Use Each

| Situation | Use Merge | Use Rebase |
|-----------|-----------|------------|
| Feature branch onto main | Yes (merge commit) | Yes (clean history) |
| Shared branch (multiple contributors) | Yes | Never rebase shared branches |
| Updating feature branch with main changes | Either | Preferred (linear history) |
| Resolving conflicts | Either | Slightly more complex |
| Public/open-source repos | Squash merge (common) | Less common |
| Preserving exact history | Yes | No (rewrites history) |

### Merge Strategies

```bash
# Regular merge (preserves branch history)
git checkout main
git merge feature/PROJ-123 --no-ff
# Creates: main ──●──●──M──  (M = merge commit)
#                   \  /
#                    ●──●    (feature commits visible)

# Squash merge (all changes in one commit)
git checkout main
git merge --squash feature/PROJ-123
git commit -m "feat: add user authentication (#123)"
# Creates: main ──●──●──S──  (S = single squash commit)
#                            (feature commits collapsed)

# Fast-forward merge (linear, no merge commit)
git checkout main
git merge --ff-only feature/PROJ-123
# Creates: main ──●──●──●──●──  (feature commits inline)
```

### Rebase Workflow

```bash
# Update feature branch with latest main
git checkout feature/PROJ-123
git fetch origin
git rebase origin/main

# Interactive rebase (clean up commits before PR)
git rebase -i HEAD~4

# In the editor:
pick abc1234 feat(auth): add login endpoint
squash def5678 fix typo
squash ghi9012 address review comments
pick jkl3456 test(auth): add login tests

# Result: 2 clean commits instead of 4
```

### The Golden Rule of Rebasing

**Never rebase branches that others have based work on.** Rebasing rewrites commit history (new SHA hashes). If someone else has pulled your branch, rebasing will cause conflicts and duplicated commits.

```bash
# SAFE: Rebase YOUR feature branch onto main
git checkout my-feature
git rebase main

# DANGEROUS: Rebase a branch others are using
git checkout shared-feature  # Others have this checked out
git rebase main              # DON'T DO THIS
```

## Protected Branches

### GitHub Branch Protection Rules

```yaml
# Repository Settings → Branches → Branch protection rules
Branch name pattern: main

# Recommended settings:
require_pull_request_reviews:
  required_approving_review_count: 1     # At least 1 approval
  dismiss_stale_reviews: true            # Re-review after new pushes
  require_code_owner_reviews: true       # CODEOWNERS must approve
  require_last_push_approval: true       # Pusher cannot self-approve

require_status_checks:
  strict: true                           # Branch must be up to date
  contexts:
    - "ci/tests"                         # All tests pass
    - "ci/lint"                          # Linting passes
    - "ci/build"                         # Build succeeds
    - "security/snyk"                    # No critical vulnerabilities

restrictions:
  enforce_admins: true                   # Admins follow rules too
  allow_force_pushes: false              # Never force push to main
  allow_deletions: false                 # Cannot delete main
  required_linear_history: false         # Allow merge commits (or true for rebase-only)

# Merge options:
allow_merge_commits: true
allow_squash_merging: true               # Squash + merge for features
allow_rebase_merging: true
```

### CODEOWNERS

```
# .github/CODEOWNERS

# Default owners for everything
* @myorg/engineering

# Frontend changes
/src/frontend/     @myorg/frontend-team
*.css              @myorg/frontend-team
*.tsx              @myorg/frontend-team

# Backend changes
/src/api/          @myorg/backend-team
/src/services/     @myorg/backend-team

# Infrastructure
/terraform/        @myorg/platform-team
/k8s/              @myorg/platform-team
Dockerfile         @myorg/platform-team

# Security-sensitive files
/src/auth/         @myorg/security-team
*.env.example      @myorg/security-team

# Documentation
/docs/             @myorg/tech-writers @myorg/engineering
```

## Monorepo Strategies

### Nx

```json
// nx.json
{
  "npmScope": "myorg",
  "tasksRunnerOptions": {
    "default": {
      "runner": "nx/tasks-runners/default",
      "options": {
        "cacheableOperations": ["build", "lint", "test"]
      }
    }
  },
  "targetDefaults": {
    "build": {
      "dependsOn": ["^build"]
    }
  }
}
```

```bash
# Only build/test affected projects
nx affected --target=build --base=main
nx affected --target=test --base=main

# Dependency graph
nx graph
```

### Turborepo

```json
// turbo.json
{
  "$schema": "https://turbo.build/schema.json",
  "globalDependencies": ["**/.env"],
  "pipeline": {
    "build": {
      "dependsOn": ["^build"],
      "outputs": ["dist/**", ".next/**"]
    },
    "test": {
      "dependsOn": ["build"],
      "outputs": []
    },
    "lint": {
      "outputs": []
    }
  }
}
```

```bash
# Only run tasks for changed packages
turbo run build --filter=...[origin/main]
turbo run test --filter=...[origin/main]
```

### Path-Based Code Owners in Monorepos

```
# .github/CODEOWNERS for monorepo
/packages/web/          @myorg/frontend-team
/packages/api/          @myorg/backend-team
/packages/shared/       @myorg/frontend-team @myorg/backend-team
/packages/mobile/       @myorg/mobile-team
/infra/                 @myorg/platform-team
```

## Recovery Patterns

### Reflog (Undo Almost Anything)

```bash
# View recent history (including resets, rebases, etc.)
git reflog

# Output:
# abc1234 HEAD@{0}: reset: moving to HEAD~3
# def5678 HEAD@{1}: commit: feat: add search
# ghi9012 HEAD@{2}: commit: fix: correct typo
# jkl3456 HEAD@{3}: commit: feat: add login

# Undo the reset
git reset --hard def5678

# Recover a deleted branch
git reflog
# Find the commit where the branch was
git checkout -b recovered-branch abc1234
```

### Cherry-Pick

```bash
# Apply a specific commit to current branch
git cherry-pick abc1234

# Cherry-pick multiple commits
git cherry-pick abc1234 def5678 ghi9012

# Cherry-pick without committing (stage changes only)
git cherry-pick --no-commit abc1234

# Cherry-pick a merge commit (specify parent)
git cherry-pick -m 1 abc1234
```

### Bisect (Find the Commit That Broke Things)

```bash
# Start bisect
git bisect start

# Mark current commit as bad
git bisect bad

# Mark a known good commit
git bisect good v2.0.0

# Git checks out a commit halfway between good and bad
# Test it, then mark:
git bisect good   # or
git bisect bad

# Repeat until the offending commit is found
# Git outputs: "abc1234 is the first bad commit"

# End bisect
git bisect reset

# Automated bisect with a test script
git bisect start HEAD v2.0.0
git bisect run npm test
```

### Other Recovery Commands

```bash
# Undo last commit (keep changes staged)
git reset --soft HEAD~1

# Undo last commit (keep changes unstaged)
git reset HEAD~1

# Discard all local changes (DANGEROUS)
git checkout -- .
git clean -fd

# Restore a deleted file from a specific commit
git checkout abc1234 -- path/to/deleted-file.ts

# Amend the last commit message
git commit --amend -m "corrected message"

# Revert a commit (creates a new "undo" commit)
git revert abc1234
```

## Conventional Commits

### Format

```
<type>(<scope>): <description>

[optional body]

[optional footer(s)]
```

### Types

| Type | Description | SemVer |
|------|-------------|--------|
| `feat` | New feature | MINOR |
| `fix` | Bug fix | PATCH |
| `docs` | Documentation only | - |
| `style` | Code style (formatting, semicolons) | - |
| `refactor` | Code change that neither fixes a bug nor adds a feature | - |
| `perf` | Performance improvement | PATCH |
| `test` | Adding or correcting tests | - |
| `chore` | Build process, tooling, dependencies | - |
| `ci` | CI configuration changes | - |
| `build` | Build system changes | - |
| `revert` | Revert a previous commit | - |

### Examples

```bash
# Feature
git commit -m "feat(auth): add OAuth2 login with Google"

# Bug fix
git commit -m "fix(cart): prevent negative quantity values"

# Breaking change
git commit -m "feat(api)!: change response format to JSON:API

BREAKING CHANGE: All API responses now use JSON:API format.
Clients must update their response parsing logic."

# With ticket reference
git commit -m "fix(payment): handle timeout in Stripe webhook

Closes #456"
```

## Semantic Versioning (SemVer)

```
MAJOR.MINOR.PATCH

2.1.3
│ │ └── Patch: backward-compatible bug fixes
│ └──── Minor: backward-compatible new features
└────── Major: breaking changes

Pre-release: 2.1.3-beta.1
Build metadata: 2.1.3+build.456
```

### Version Bumping with Conventional Commits

```bash
# Using standard-version or release-please
# feat commit → bumps MINOR
# fix commit → bumps PATCH
# BREAKING CHANGE → bumps MAJOR

# Automate with CI:
npx standard-version          # Bump version, update CHANGELOG
npx standard-version --dry-run  # Preview without changes
```

## Best Practices

1. **Commit early, commit often** -- small commits are easier to review, revert, and bisect
2. **Write meaningful commit messages** -- future you will thank present you
3. **Keep branches short-lived** -- merge within 1-3 days; long branches = merge pain
4. **Rebase before PR** -- clean, linear history is easier to read and bisect
5. **Squash merge for features** -- one clean commit per feature on main
6. **Protect main** -- require reviews, passing tests, and up-to-date branches
7. **Use CODEOWNERS** -- ensure the right people review the right code
8. **Never force push to shared branches** -- only force push to your own feature branches
9. **Use `.gitignore`** effectively -- never commit `node_modules`, `.env`, build artifacts
10. **Tag releases** -- `git tag v2.1.0` with annotated tags for release history

## Anti-Patterns

1. **Committing to main directly** -- bypasses review and CI
2. **"WIP" commits on main** -- main should always be deployable
3. **Giant PRs** (500+ lines) -- break into smaller, reviewable chunks
4. **Long-lived feature branches** -- leads to "merge hell" and integration surprises
5. **Force pushing to shared branches** -- destroys others' work
6. **Commit messages like "fix" or "update"** -- provide context for future maintainers
7. **Not using `.gitignore`** -- `node_modules/` in the repo is a common mistake
8. **Rebasing public history** -- rewrites shared commits, causing confusion
9. **No branch protection** -- anyone can push broken code to main
10. **Mixing unrelated changes** in a single commit -- makes review and revert difficult

## Sources & References

- https://nvie.com/posts/a-successful-git-branching-model/ -- Git Flow (original post by Vincent Driessen)
- https://docs.github.com/en/get-started/using-github/github-flow -- GitHub Flow
- https://trunkbaseddevelopment.com/ -- Trunk-Based Development
- https://www.conventionalcommits.org/ -- Conventional Commits specification
- https://semver.org/ -- Semantic Versioning specification
- https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches -- Branch protection rules
- https://nx.dev/getting-started/intro -- Nx monorepo documentation
- https://turbo.build/repo/docs -- Turborepo documentation
- https://git-scm.com/book/en/v2 -- Pro Git book (free)
