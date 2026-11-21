# Contributing to Jhon

Thanks for wanting to help. Jhon is maintained by one person, so the process
below keeps every change easy to review.

## Questions, ideas and bugs

- **A question or an idea to talk through:** open a Discussion.
- **A bug:** open an issue with the bug form. Say what you ran, what you
  expected and what happened, with your `jhon version` and runtime.
- **A proposal:** open an issue with the proposal form before you write
  code, so we can agree on the shape first.
- **A security problem:** never in public; see [SECURITY.md](SECURITY.md).

## Pull requests

1. Start from an issue we agreed on, unless the fix is small and obvious
   (a typo, a broken link, a one-line bug).
2. Keep a pull request to one change. A large change is easier to review as
   a few smaller pull requests.
3. Behaviour changes come with tests, written first: the README's "Working on
   Jhon" section describes the workflow and the checks.
4. Run the checks before you push:

   ```sh
   go build -o build/jhon ./engine/cmd/jhon
   ./build/jhon validate
   ./build/jhon test
   ```

   If you changed the shell, also run `npm test` and `npx tsc --noEmit -p .`
   in `shell/`.
5. Update the docs and the help text that describe what you changed.
6. Use a [Conventional Commits](https://www.conventionalcommits.org) message,
   such as `fix(guards): …` or `docs: …`.

Every pull request needs the CI checks green and the maintainer's review
before it is merged.

## License

Jhon is licensed under the [Apache License 2.0](LICENSE). By contributing,
you agree that your contribution is licensed under the same terms.

## Conduct

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md).
