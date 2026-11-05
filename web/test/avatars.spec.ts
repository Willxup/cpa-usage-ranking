import { expect, test } from '@playwright/test'

test('serves the embedded catalog and decodes every approved avatar in the production browser', async ({ page, request }) => {
  const response = await request.get('/avatars/catalog.json')
  expect(response.status()).toBe(200)
  expect(response.headers()['cache-control']).toBe('no-cache')
  const catalog = await response.json() as { default_avatar_id: number, avatars: { avatar_id: number, path: string }[] }
  expect(catalog.default_avatar_id).toBe(1)
  expect(catalog.avatars).toHaveLength(66)
  await page.goto('/')
  for (const [index, avatar] of catalog.avatars.entries()) {
    expect(avatar.avatar_id).toBe(index + 1)
    expect(avatar.path).toBe(`images/${String(index + 1).padStart(3, '0')}.webp`)
    const size = await page.evaluate(async (src) => {
      const image = new Image()
      image.src = `/avatars/${src}`
      await image.decode()
      return [image.naturalWidth, image.naturalHeight]
    }, avatar.path)
    expect(size).toEqual([128, 128])
  }
  const image = await request.get('/avatars/images/001.webp')
  expect(image.headers()['content-type']).toBe('image/webp')
  expect(image.headers()['cache-control']).not.toContain('immutable')
  const cached = await request.get('/avatars/images/001.webp', { headers: { 'If-None-Match': image.headers().etag } })
  expect(cached.status()).toBe(304)
  for (const path of ['/avatars/images/067.webp', '/avatars/SOURCE.original.md', '/avatars/images/']) {
    const missing = await request.get(path)
    expect(missing.status()).toBe(404)
    expect(missing.headers()['content-type']).toContain('application/json')
  }
})
