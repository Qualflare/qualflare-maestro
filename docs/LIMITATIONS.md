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
- **Shard ids.** With `--shard-all`, ids gain `@<device>` only when a flow runs on more than one
  device, and `#<k>` only when those devices share a model. `--shard-split` ids are unchanged.
- **Variable values.** Values passed with `--env` and `MAESTRO_*` environment variables are replaced
  by `${NAME}` in errors, descriptions, properties, labels and step text. Values shorter than 4
  characters are not replaced, and values set any other way — a flow's own `env:` block,
  `evalScript`, a file loaded by a script — are unknown to the reporter and are not protected.
- **Maestro settings are not secrets.** `MAESTRO_CLI_*`, `MAESTRO_DRIVER_*`, `MAESTRO_USE_*`,
  `MAESTRO_DISABLE_*` and `MAESTRO_VERSION` are not treated as secrets. The values `true` and
  `false` are never replaced either.
- **No JUnit file is left behind.** The reporter owns `--output` and removes its working directory once it
  has read Maestro's results, so a run cannot also leave a JUnit XML file for something else to render —
  GitLab's Tests tab, for instance. Run Maestro directly for that, and upload the XML with
  `qf <project> collect <file> --format maestro`.
- **Argument files are opaque.** Values passed inside a picocli `@argfile` are not seen by the
  reporter and are not protected.
- **Unattributed errors use the log tail.** When Maestro exits without writing results, the report's
  error text is the end of Maestro's log and can include the device id and local paths.
- **Windows binary paths.** Name the Maestro binary with `QUALFLARE_MAESTRO_BIN`; an explicit
  `maestro.bat` path in the arguments is not recognised as the binary.
- **Shorthand reporter flags.** In shorthand form, a flag that is also a reporter flag, such as
  `-platform`, is taken by the reporter. Use the `--` form to pass it to Maestro.
- **Platform detection** reads Maestro's debug output first: a per-flow `manifest.json` lists a
  device log sourced from logcat on Android and from xctest on Apple platforms, which no naming
  choice can break. Only if that is unavailable does it fall back to the device name, then to `ios`
  with a warning asking for `-platform`.
- **On the 2.6.x flat layout, Android needs `-platform android`.** That layout writes no manifest,
  and Maestro names an Android device after its AVD or its adb serial — `Pixel_7_API_34`,
  `R5CT30ABCDE` — so there is nothing to detect. A real iPhone is the same shape of problem: its
  device string carries a bare version number rather than `iOS`, and only lands on `ios` because
  that is the fallback.
- **Android is measured on the 2.10 bundle layout only.** `test/captures/maestro-2.10.0-android14/`
  is a real API 34 emulator run; the flows, steps, screenshots and layout match iOS. Android on
  2.6.x, and any physical device, remain uncaptured.
- **Screenshots need `qf` 0.1.24 or newer** to upload.
