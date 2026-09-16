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
