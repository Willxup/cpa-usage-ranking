import { expect, test } from '@playwright/test'

test('registers and safely retries against the real production binary without adding account UI', async ({ page, request }) => {
  await page.goto('/')
  const result = await page.evaluate(async () => {
    // 测试客户端先生成并保存秘密；不把凭据交给服务端签发，也不加入生产 UI。
    const raw = crypto.getRandomValues(new Uint8Array(32))
    const secret = btoa(String.fromCharCode(...raw)).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '')
    localStorage.setItem('registration-test-secret', secret)
    const send = async (body: object) => {
      const response = await fetch('/v1/clients/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${localStorage.getItem('registration-test-secret')}` },
        body: JSON.stringify(body),
      })
      return { status: response.status, cache: response.headers.get('Cache-Control'), body: await response.json() as Record<string, unknown> }
    }
    const first = await send({ display_name: '  Ｂrowser 🙂  ', avatar_id: 66 })
    const retry = await send({ display_name: '\u0000', avatar_id: -1 })
    const minimal = await send({})
    const day = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date())
    const snapshot = {
      schema_version: 1, revision: '1', items: [{
        model: 'claude-fable-5', total_tokens: '10', input_tokens: '8', output_tokens: '2',
        cache_read_tokens: '3', cache_write_tokens: '1', reasoning_tokens: '1',
        request_count: '2', success_count: '1', ttft_avg_ms: null, ttft_samples: '0',
        latency_avg_ms: 2, latency_samples: '2',
      }],
    }
    const report = async () => {
      const response = await fetch(`/v1/reports/${day}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${localStorage.getItem('registration-test-secret')}` },
        body: JSON.stringify(snapshot),
      })
      return { status: response.status, body: await response.json() as Record<string, unknown> }
    }
    const firstReport = await report()
    const retryReport = await report()
    localStorage.removeItem('registration-test-secret')
    return { first, retry, minimal, firstReport, retryReport }
  })
  expect(result.first.status).toBe(201)
  expect(result.first.cache).toBe('no-store')
  expect(Object.keys(result.first.body).sort()).toEqual(['avatar_id', 'client_id', 'display_name'])
  expect(result.first.body.display_name).toBe('Browser 🙂')
  expect(result.first.body.avatar_id).toBe(66)
  expect(result.retry.status).toBe(200)
  expect(result.minimal.status).toBe(200)
  expect(result.retry.body).toEqual(result.first.body)
  expect(result.minimal.body).toEqual(result.first.body)
  expect(result.firstReport).toEqual({ status: 200, body: { revision: '1' } })
  expect(result.retryReport).toEqual(result.firstReport)
  const rejected = await request.post('/v1/clients/register', { data: { display_name: 'No secret' } })
  expect(rejected.status()).toBe(401)
  expect(await rejected.json()).toEqual({ error: { code: 'invalid_credential', message: 'Invalid client credential' } })
  await expect.poll(async () => {
    const response = await request.get('/v1/leaderboards/overall/today')
    if (response.status() !== 200) return false
    const data = await response.json() as { entries: { display_name: string }[] }
    return data.entries.some((row) => row.display_name === 'Browser 🙂')
  }).toBe(true)
  await page.reload()
  await expect(page.getByText('Browser 🙂')).toBeVisible()
  await expect(page.getByRole('button', { name: /注册|登录/ })).toHaveCount(0)
})
