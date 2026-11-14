import { DatabaseSync } from 'node:sqlite'
import { resolve } from 'node:path'

// Only this test process's isolated SQLite file is seeded. The real service
// reads these registered-user fixtures; no HTTP list response is mocked and no
// production registration/login limit is changed.
export function seedUsers(prefix: string, count: number) {
  const output = process.env.PLAYWRIGHT_OUTPUT_DIR
  const port = process.env.RANKING_TEST_PORT
  if (!process.env.RANKING_TEST_ADMIN_CONFIG || !output || !port) throw new Error('Isolated admin test database required')
  const db = new DatabaseSync(resolve(output, '..', 'databases', port, 'ranking.db'))
  try {
    db.exec('PRAGMA busy_timeout=5000; BEGIN IMMEDIATE')
    const insert = db.prepare('INSERT INTO clients(token_hash,display_name,avatar_id,disabled,created_at_ms) VALUES(randomblob(32),?,?,?,?)')
    const users: { id: string; name: string }[] = []
    for (let index = 0; index < count; index++) {
      const name = `${prefix}-${index}${index === 0 ? ' <img>' : ''}`
      const result = insert.run(name, 1, index === 0 ? 1 : 0, Date.now())
      users.push({ id: String(result.lastInsertRowid), name })
    }
    db.exec('COMMIT')
    return users
  } finally { db.close() }
}
