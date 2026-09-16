# Known limitations

- **Maestro 2.6.0 or newer.** Older releases write no `file` attribute in JUnit, which case ids
  depend on.
- **Only `maestro test`.** `maestro cloud`, `maestro record` and `--continuous` runs are not wrapped.
- **Shorthand names.** A flow folder or file literally named `test` or `maestro`, given as the first
  argument of the shorthand form, is read as the Maestro subcommand or binary; write `./test`
  instead, or use the `--` form.
- **No retry history per case.** Maestro has no flow-level retry for local runs. Retries done by the
  YAML `retry` command appear as steps on Maestro 2.10+, where each attempt is recorded; on older
  releases each attempt overwrites the last.
- **Nesting needs Maestro 2.10+.** Older releases record no depth, so `runFlow`, `repeat` and `retry`
  children are shown as top-level steps.
- **Screenshots.** Maestro 2.10+ takes one for failed and warned steps and links it to the step.
  Older releases take one for a failed step only, matched to the flow by file name.
- **Screenshot names.** Screenshot attachments are named after the copied file, not after the step.
  Maestro's own file names can contain a command's argument, including a variable's value.
- **Flows need unique names.** Maestro names its debug output after the flow. When two flows in one
  run share a name — or one flow runs on several devices with `--shard-all` — their steps cannot be
  told apart, so those cases keep their result but get no steps or screenshots.
- **Same-model shards.** With `--shard-all` on several devices of the same model, cases get a `#<k>`
  suffix (the device's position in the run) so their histories stay separate; if Maestro ever
  reorders shards between runs, those histories can mix.
- **Variable values.** Values passed with `--env` and `MAESTRO_*` environment variables are replaced
  by `${NAME}` in errors, descriptions, properties, labels and step text. Values shorter than 4
  characters are not replaced, and values set any other way — a flow's own `env:` block,
  `evalScript`, a file loaded by a script — are unknown to the reporter and are not protected.
- **Platform detection** reads Maestro's device name. If it cannot tell, the report says `ios` and a
  warning asks for `-platform`.
- **Only iOS has been measured.** Android output is expected to match, but has not been captured yet.
- **Screenshots need `qf` 0.1.24 or newer** to upload.
