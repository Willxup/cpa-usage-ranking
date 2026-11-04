// Raster exports are generated from our SVG masters, using the existing test browser.
import { chromium } from '@playwright/test'
import { Buffer } from 'node:buffer'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const publicDir = new URL('../public/', import.meta.url)
const outputDir = new URL('assets/brand/', publicDir)
await mkdir(outputDir, { recursive: true })
const browser = await chromium.launch()
try {
  const page = await browser.newPage({ deviceScaleFactor: 1 })
  const render = async (source, size, filename) => {
    const svg = await readFile(new URL(source, publicDir), 'utf8')
    await page.setViewportSize({ width: size, height: size })
    await page.setContent(`<style>html,body{margin:0;background:transparent}svg{display:block;width:100vw;height:100vh}</style>${svg}`)
    return page.screenshot({ path: fileURLToPath(new URL(filename, outputDir)), omitBackground: true })
  }
  for (const size of [180, 192, 512]) await render('assets/brand/project-icon.svg', size, `project-icon-${size}.png`)
  const sizes = [16, 32, 48]
  const images = []
  for (const size of sizes) images.push(await render('favicon.svg', size, `favicon-${size}.png`))
  // ICO directory with lossless PNG entries; no image-processing dependency required.
  const header = Buffer.alloc(6 + images.length * 16)
  header.writeUInt16LE(1, 2)
  header.writeUInt16LE(images.length, 4)
  let offset = header.length
  images.forEach((image, index) => {
    const entry = 6 + index * 16
    header[entry] = sizes[index]
    header[entry + 1] = sizes[index]
    header.writeUInt16LE(1, entry + 4)
    header.writeUInt16LE(32, entry + 6)
    header.writeUInt32LE(image.length, entry + 8)
    header.writeUInt32LE(offset, entry + 12)
    offset += image.length
  })
  await writeFile(new URL('favicon.ico', outputDir), Buffer.concat([header, ...images]))
  console.log('Exported project PNGs (180/192/512) and favicon PNGs/ICO (16/32/48).')
} finally {
  await browser.close()
}
