# Releasing

There is no version file. The version comes from the git tag, through `-ldflags` for release builds
and through the module version for `go install`.

1. Make sure `main` is green, including both Maestro jobs in CI and the latest E2E run.
2. Move the `Unreleased` entries in `CHANGELOG.md` under the new version with today's date, commit,
   and push.
3. Tag and push:

   ```bash
   git tag -a v0.1.0 -m "v0.1.0" && git push origin v0.1.0
   ```

4. `release.yml` runs the full gate, checks the built binary reports the tag, then goreleaser
   publishes the archives, checksums, SBOMs, cosign signature, GitHub release and Homebrew cask.
   Without a `HOMEBREW_TAP_TOKEN` secret the cask is skipped rather than failing the release.
5. Verify the result, not the green check:

   ```bash
   brew install qualflare/tap/qualflare-maestro && qualflare-maestro -version
   go install github.com/Qualflare/qualflare-maestro/cmd/qualflare-maestro@v0.1.0 && qualflare-maestro -version
   ```

   Both should print the tag's version.

## After the release

Sibling reporters shipped with no discovery work at all
(`../qualflare-go/RELEASING.md` says so out loud), so where this one is listed is
recorded here rather than rediscovered each time.

- **Nothing to do for pkg.go.dev or the module proxy.** They index the tag
  automatically. Confirm with
  `curl -s https://proxy.golang.org/github.com/!qualflare/qualflare-maestro/@v/list`.
- **`go install` has a trap this repo actually hit.** The module zip format
  rejects any non-ASCII character in a path, and Maestro names a failure
  screenshot `screenshot-❌-<ms>-(<flow>).png`. `test/captures/go.mod` prunes that
  directory from the module; keep it. v0.1.0-rc.1 is what caught this, which is
  the argument for cutting an rc before a first release.
- **The one-line description every listing reuses**, so it stays identical
  across the awesome lists, the repo description and the docs:

  > Native Maestro reporter for Qualflare — a step per command, nested under
  > `runFlow`/`repeat`/`retry`, Maestro's screenshots on the step that took them,
  > tags and metadata from the flow's YAML, and a report even when a run dies
  > before Maestro writes one.

- **Listings, with their dates and gates** (PRs opened 2026-09-20):
  - `ludovicobesana/awesome-maestro` — the community list Maestro's own docs link
    to, section **Tools and Integrations**. No eligibility bar; BrowserStack is
    already listed, so a vendor tool is in scope. **PR #8.**
  - `mobile-dev-inc/maestro-docs` — the official community page,
    `resources/community/community-projects.md`, section **DevOps and CI/CD**
    (peers: a Fastlane plugin, Codemagic, a Slack results poster). Entries use
    `* [Name](url): Description.` — colon, not dash. **PR #204.**
  - `ZoranPandovski/awesome-testing-tools` — **Mobile Testing Tools**, which
    already lists Maestro. Merges monthly, and its contributing file asks for one
    PR per suggestion, so a second PR alongside the older #116 is what it wants
    rather than stacking. **PR #150.**
  - `vsouza/awesome-ios` — their CONTRIBUTING requires a repo **30+ days old**.
    This one was published 2026-09-18, so **not before 2026-10-18**; the list is
    Swift-library oriented, so weigh fit before spending the submission.
  - `atinfo/awesome-test-automation` — `mobile-test-automation.md` under
    **Continuous Integration** (Maestro is absent from the file entirely, which
    the PR offers to fix separately). **PR #596**, alongside the older #572 and
    #594 on a queue that has not moved in months.
  - **Dead or wrong-fit, checked 2026-09-20 so nobody re-checks:**
    `hotchemi/awesome-android-testing` is archived (last push 2021);
    `jondot/awesome-react-native` has merged nothing since April 2021 despite
    recent commits, so its 21 open PRs are decoration; `Solido/awesome-flutter`
    takes only Dart/Flutter-specific packages, and this reporter behaves
    identically for a native Android app; `matteocrippa/awesome-swift` is Swift
    libraries only.
  - `TheJambo/awesome-testing` — **excluded.** Three PRs closed unmerged; the
    maintainer's bar is demonstrated usage, not persistence.
- **Anything posted inside Maestro's own community** — the Slack at
  `slack.maestro.dev`, `mobile-dev-inc/maestro` Discussions — is the maintainer's
  to send, not an agent's. Draft and hand over.
- **Every submission discloses the affiliation.** Qualflare is a commercial
  product and this is its client. Lists tolerate vendor tools; they do not
  tolerate finding out later.
