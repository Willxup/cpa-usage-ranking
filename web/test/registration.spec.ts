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
    localStorage.removeItem('registration-test-secret')
    return { first, retry, minimal }
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
  const rejected = await request.post('/v1/clients/register', { data: { display_name: 'No secret' } })
  expect(rejected.status()).toBe(401)
  expect(await rejected.json()).toEqual({ error: { code: 'invalid_credential', message: 'Invalid client credential' } })
  expect((await request.get('/readyz')).status()).toBe(503)
  await expect(page.getByRole('heading', { name: '等待第一份真实数据' })).toBeVisible()
  await expect(page.getByRole('button', { name: /注册|登录/ })).toHaveCount(0)
})
