#!/bin/bash
# Hook: Run tests before allowing git commit
# Install: Add to .claude/settings.json hooks.pre_commit

echo "Running tests before commit..."

# Detect project type and run appropriate test command
if [ -f "package.json" ]; then
  if command -v pnpm &> /dev/null; then
    pnpm test 2>&1
  elif command -v npm &> /dev/null; then
    npm test 2>&1
  fi
elif [ -f "Gemfile" ]; then
  bundle exec rspec 2>&1
elif [ -f "pubspec.yaml" ]; then
  flutter test 2>&1
elif [ -f "requirements.txt" ] || [ -f "pyproject.toml" ]; then
  python -m pytest 2>&1
fi

EXIT_CODE=$?

if [ $EXIT_CODE -ne 0 ]; then
  echo ""
  echo "Tests failed! Commit blocked."
  echo "Fix the failing tests and try again."
  exit 1
fi

echo "All tests passed. Proceeding with commit."
exit 0
