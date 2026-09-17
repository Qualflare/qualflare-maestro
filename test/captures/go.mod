// Not a real module. This file exists so `go install` works.
//
// Maestro names a failure screenshot `screenshot-❌-<ms>-(<flow>).png`, and the module
// zip format rejects a path containing that emoji:
//
//   create zip: malformed file path ".../screenshot-❌-...png": invalid char '❌'
//
// A nested go.mod prunes this directory from the parent module, so the captures stay
// byte-identical to what Maestro wrote while `go install
// github.com/Qualflare/qualflare-maestro/cmd/qualflare-maestro@vX.Y.Z` still resolves.
// Nothing here is Go code, and the tests read these files by relative path from the
// repository, not through the module.
module github.com/Qualflare/qualflare-maestro/test/captures

go 1.21
