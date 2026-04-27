# Skill Answer Guidelines

Answer guidelines provide concrete expected-answer criteria for skill tests. When present, the `skill-tester-agent` evaluates responses against these checklists instead of scoring on vibes alone.

## How It Works

1. Each file in this directory corresponds to a skill prompt in `scripts/skill-prompts/`.
2. During `skill-agent-test.sh`, if `scripts/skill-answers/{skill}.txt` exists, it is appended to the scorer prompt.
3. The evaluator checks each point and includes `guideline_coverage` in its JSON output.

## File Naming

Files must match the skill prompt name exactly:

```
scripts/skill-prompts/flutter-architecture.txt   # The prompt
scripts/skill-answers/flutter-architecture.txt   # The answer guideline
```

## Format

```
# Answer Guidelines: {skill-name}

## Must Cover
- Point the response MUST address (one per line)
- Each point should be specific and verifiable
- Reference concrete patterns, APIs, or techniques from the SKILL.md

## Must NOT Do
- Anti-pattern the response must NOT recommend
- Outdated practice that should be avoided
- Common mistake that would indicate the skill isn't working

## Code Examples Must Include
- Specific code pattern that must appear in the response
- Name the class, function, or construct expected
- Reference syntax or idiom that proves framework-specific knowledge
```

## Writing Guidelines

- **Be specific**: "Use `StateNotifier` with immutable state via `copyWith`" is better than "Use state management".
- **Derive from SKILL.md**: Every point should trace back to a pattern or recommendation in the corresponding `skills/{skill}/SKILL.md`.
- **Match the prompt**: Points should be relevant to what `scripts/skill-prompts/{skill}.txt` actually asks.
- **Keep it concise**: 4-8 "Must Cover" points, 2-4 "Must NOT Do" points, 2-4 "Code Examples" points.
- **Avoid overlap**: Each point should test a distinct aspect of the skill.

## Scoring

The evaluator uses these guidelines to produce a `guideline_coverage` object:

```json
{
  "guideline_coverage": {
    "must_cover": { "total": 4, "covered": 4, "missed": [] },
    "must_not_do": { "total": 3, "violations": [] },
    "code_examples": { "total": 3, "present": 3, "missing": [] }
  }
}
```

- **must_cover**: Each point scored as covered or missed
- **must_not_do**: Each point checked for violations
- **code_examples**: Each required pattern checked for presence

## Contributing

1. Read the SKILL.md: `skills/{skill}/SKILL.md`
2. Read the prompt: `scripts/skill-prompts/{skill}.txt`
3. Write the answer guideline based on what a correct response to that prompt should contain
4. Save as `scripts/skill-answers/{skill}.txt`
