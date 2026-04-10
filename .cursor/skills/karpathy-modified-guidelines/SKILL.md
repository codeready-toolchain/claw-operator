---
name: karpathy-modified-guidelines
description: Behavioral guidelines to reduce common LLM coding mistakes by emphasizing thinking before coding, simplicity, surgical changes, and goal-driven execution. Use when implementing features, fixing bugs, refactoring code, or performing any coding task.
---

# Karpathy Guidelines

Behavioral guidelines to reduce common LLM coding mistakes. Apply these principles to all coding tasks.

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

## 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

## 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- No documentation files (MD, reports, etc.) unless explicitly requested.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

## 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't go on unrelated cleanup sprees, but fix obvious issues (bugs, stale comments) adjacent to your changes.
- Don't refactor things that aren't broken.
- Use modern language idioms — follow project skills over legacy patterns in the file.
- Match existing code style (naming, formatting) unless it conflicts with best practices.

When your changes create orphans:
- Remove imports/variables/functions that your changes made unused.

The test: Every changed line should trace directly to the user's request, or to using a modern idiom the project's skills require.

## 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Implement checks, verify existing tests still pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

## Applying the Guidelines

When working on a task:

1. **Before starting**: State assumptions and ask clarifying questions
2. **While coding**: Keep it minimal - resist over-engineering
3. **During changes**: Make surgical edits, use modern idioms in new/changed code
4. **Throughout**: Define success criteria and verify against them

These guidelines help prevent common pitfalls:
- Over-complicating simple tasks
- Making assumptions instead of asking
- Introducing unnecessary changes
- Losing sight of the original goal
