# AGENTS.md

Read `docs/spec.md` (requirements, security rules, build order) and
`docs/decisions.md` (where the build differs from the spec) before changing
anything.

- Never commit, push, tag or sign. The owner reviews and commits by hand
  (commits are signed with biometric SSH). Leave changes in the working tree.
- Tooling comes from `mise.toml` and commands from the `justfile`. Don't
  install global tools.
- Before handing work back, run `just lint` and `just test`. If output changes
  on purpose, run `just golden` and review the diff.
- Go stdlib first. The only allowed dependencies are goldmark and chroma/v2.
  Ask before adding others. Shell out to `git`, no go-git.
- Keep Go under ~1,800 lines (`just loc`) and each change under ~400 lines.
- The security rules in the spec are hard requirements. When unsure, return
  404 and never show a private repo.
- Record new decisions in `docs/decisions.md`.
