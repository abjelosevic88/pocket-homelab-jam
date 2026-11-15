# Backup

Everything lives in `DATA_DIR`: the SQLite file `pocket-jam.db` (WAL mode) and the audio in `media/<user id>/` (every take and mix as a file). Take a consistent copy of the database with `pocket-jam backup` (writes `DATA_DIR/backups/pocket-jam-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin), and back up the `media/` folder beside it. Back up that copy, not the live file. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-jam.db` (remove the `-wal`/`-shm` files) and `media/`, start.

Every person can also export everything they made as a `.zip` with the audio (songs, tracks, every take and mix, the time spent) or as JSON without it (Settings → Backup & data), and bring it back on any box; importing the same file twice adds nothing twice.
