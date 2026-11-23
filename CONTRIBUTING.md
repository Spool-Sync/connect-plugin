# Contributing

## Where this project lives

This project is maintained on GitLab:
**https://gitlab.honex.life/spoolsync/spool-sync-prusa-connect**

The GitHub repository, https://github.com/Spool-Sync/connect-plugin,
is a read-only mirror. GitLab pushes every branch and tag to it, so anything
committed on GitHub directly is overwritten by the next push. Please open
issues and merge requests on GitLab. A pull request opened on GitHub is not
merged there; it may be carried over to GitLab by hand.

## Making a change

1. Fork the project on GitLab, or ask for access, and branch from `main`.
2. Make the change with its tests.
3. Check it:

   ```sh
   go vet ./...
   go test -race ./...
   cd ui && pnpm install && pnpm typecheck && pnpm test
   ```

   The database tests run on SQLite always, and on Postgres too when
   `SPOOLSYNC_TEST_POSTGRES_DSN` names a server they may create databases on.
4. Open a merge request against `main`.

Commit subjects follow Conventional Commits (`feat:`, `fix:`, `chore:`,
`docs:`, `test:`, `ci:`), in the imperative, and say what the change does for
a person using the module.

## Licence

The module is a derivative of Prusa Connect and is distributed under its
licence, OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1 (see [LICENSE](LICENSE)).
By contributing you agree your contribution is distributed under it too. Keep
the credit to Prusa Research in the source and in the UI.
