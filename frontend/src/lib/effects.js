import { Emitter } from '@pixi/particle-emitter'
import {
  CRTFilter,
  ColorMapFilter,
  ColorOverlayFilter,
  GlitchFilter,
  GodrayFilter,
  HslAdjustmentFilter,
  OldFilmFilter,
  PixelateFilter,
  RGBSplitFilter,
} from 'pixi-filters'
import * as PIXI from 'pixi.js'
import { H, W, effectControls, effectSettings } from './project.js'

const STEP = 1 / 30
const SEEK_GAP = 0.12

const PARTICLES = {
  snow: {
    frequency: 0.004,
    maxParticles: 700,
    lifetime: [5, 8],
    speed: [50, 130],
    rotation: [70, 110],
    spin: [-20, 20],
    scale: [0.22, 0.7],
    alpha: [0.95, 0.4],
    spawn: { x: -160, y: -80, w: W + 320, h: H + 120 },
    texture: 'flake',
  },
  rain: {
    frequency: 0.003,
    maxParticles: 650,
    lifetime: [0.65, 0.9],
    speed: [1100, 1600],
    rotation: [62, 100],
    spin: [0, 0],
    scale: [0.75, 1.25],
    alpha: [0.75, 0.4],
    spawn: { x: -220, y: -120, w: W + 480, h: H + 160 },
    texture: 'streak',
  },
  storm: {
    frequency: 0.0018,
    maxParticles: 900,
    lifetime: [0.55, 0.8],
    speed: [1400, 1900],
    rotation: [68, 108],
    spin: [0, 0],
    scale: [0.9, 1.45],
    alpha: [0.85, 0.45],
    spawn: { x: -240, y: -140, w: W + 520, h: H + 180 },
    texture: 'streak',
  },
  blizzard: {
    frequency: 0.0022,
    maxParticles: 900,
    lifetime: [1.1, 1.7],
    speed: [720, 1200],
    rotation: [8, 28],
    spin: [-50, 50],
    scale: [0.1, 0.38],
    alpha: [0.95, 0.4],
    spawn: { x: -420, y: -180, w: 460, h: H + 360 },
    texture: 'flake',
  },
  fog: {
    frequency: 0.06,
    maxParticles: 42,
    lifetime: [8, 12],
    speed: [10, 26],
    rotation: [-30, 30],
    spin: [-6, 6],
    scale: [1.6, 2.8],
    alpha: [0.2, 0.04],
    spawn: { x: -100, y: -60, w: W + 200, h: H + 120 },
    texture: 'puff',
  },
}

let app
let stage
let sprite
let baseTexture
let flash
let particleHolder
let displacementSprite
let failed = false
let warned = false

let grade
let blur
let overlay
let godray
let glitch
let rgb
let crt
let film
let colorMap
let displace
let pixelate
let noise
let hsl

const buffer = typeof document === 'undefined' ? null : document.createElement('canvas')
if (buffer) {
  buffer.width = W
  buffer.height = H
}
const bufferCtx = buffer?.getContext('2d')

const states = new Map()

function unitSeed(t) {
  const frame = Math.floor(Math.max(0, t) * 30)
  let h = Math.imul(frame + 1, 374761393)
  h = Math.imul(h ^ (h >>> 13), 1274126177)
  return ((h ^ (h >>> 16)) >>> 0) / 4294967296
}

function mulberry32(seed) {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function wrapHue(degrees) {
  let hue = degrees % 360
  if (hue > 180) hue -= 360
  if (hue < -180) hue += 360
  return hue
}

function slider(settings, key) {
  const value = Number(settings?.[key])
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value))
}

// pace is 0 at a stopped slider, 1 at 50, and 2 at 100.
function pace(settings, key = 'speed') {
  return slider(settings, key) / 50
}

// scaled maps a slider so its default position reproduces atDefault, and 0 is off.
function scaled(effectId, settings, key, atDefault) {
  const def = effectControls(effectId).find((item) => item.id === key)?.def ?? 0
  if (!def) return 0
  return (slider(settings, key) / def) * atDefault
}

function lightning(t, amount, rate) {
  if (amount <= 0 || rate <= 0) return 0
  const clock = t * rate
  let alpha = 0
  for (const period of [2.6, 6.1, 9.4]) {
    const x = clock % period
    if (x < 0.05) alpha = Math.max(alpha, 0.92)
    else if (x < 0.12) alpha = Math.max(alpha, 0.38)
  }
  return alpha * amount
}

function softCircle(size, inner, outer) {
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const g = canvas.getContext('2d')
  const mid = size / 2
  const rad = g.createRadialGradient(mid, mid, 0, mid, mid, mid)
  rad.addColorStop(0, inner)
  rad.addColorStop(1, outer)
  g.fillStyle = rad
  g.beginPath()
  g.arc(mid, mid, mid, 0, Math.PI * 2)
  g.fill()
  return PIXI.Texture.from(canvas)
}

function streakTexture() {
  const canvas = document.createElement('canvas')
  canvas.width = 80
  canvas.height = 18
  const g = canvas.getContext('2d')
  g.fillStyle = 'rgba(15, 28, 58, 0.55)'
  g.fillRect(0, 5, 80, 8)
  const grad = g.createLinearGradient(0, 0, 80, 0)
  grad.addColorStop(0, 'rgba(255,255,255,0)')
  grad.addColorStop(0.18, 'rgba(236,244,255,1)')
  grad.addColorStop(1, 'rgba(255,255,255,0.15)')
  g.fillStyle = grad
  g.fillRect(0, 6, 80, 6)
  return PIXI.Texture.from(canvas)
}

function duoMap() {
  const canvas = document.createElement('canvas')
  canvas.width = 64
  canvas.height = 1
  const g = canvas.getContext('2d')
  const grad = g.createLinearGradient(0, 0, 64, 0)
  grad.addColorStop(0, '#1a237e')
  grad.addColorStop(1, '#ff4fa3')
  g.fillStyle = grad
  g.fillRect(0, 0, 64, 1)
  return canvas
}

function waveMap() {
  const size = 128
  const canvas = document.createElement('canvas')
  canvas.width = size
  canvas.height = size
  const g = canvas.getContext('2d')
  const image = g.createImageData(size, size)
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      const i = (y * size + x) * 4
      const wave = Math.sin((y / size) * Math.PI * 4)
      image.data[i] = 128 + wave * 127
      image.data[i + 1] = 128 + Math.sin((x / size) * Math.PI * 2 + y / 18) * 50
      image.data[i + 2] = 128
      image.data[i + 3] = 255
    }
  }
  g.putImageData(image, 0, 0)
  return canvas
}

function emitterConfig(spec, textures) {
  const [speedMin, speedMax] = spec.speed
  const [scaleMin, scaleMax] = spec.scale
  const [alphaIn, alphaOut] = spec.alpha
  return {
    lifetime: { min: spec.lifetime[0], max: spec.lifetime[1] },
    frequency: spec.frequency,
    maxParticles: spec.maxParticles,
    particlesPerWave: spec.texture === 'puff' ? 2 : 1,
    emit: true,
    autoUpdate: false,
    pos: { x: 0, y: 0 },
    behaviors: [
      { type: 'alpha', config: { alpha: { list: [{ time: 0, value: alphaIn }, { time: 1, value: alphaOut }] } } },
      {
        type: 'scale',
        config: {
          scale: { list: [{ time: 0, value: scaleMax }, { time: 1, value: scaleMax * 0.85 }] },
          minMult: scaleMin / scaleMax,
        },
      },
      {
        type: 'moveSpeed',
        config: {
          speed: { list: [{ time: 0, value: speedMax }, { time: 1, value: speedMin }] },
          minMult: speedMin / speedMax,
        },
      },
      {
        type: 'rotation',
        config: { minStart: spec.rotation[0], maxStart: spec.rotation[1], minSpeed: spec.spin[0], maxSpeed: spec.spin[1], accel: 0 },
      },
      { type: 'textureSingle', config: { texture: textures[spec.texture] } },
      { type: 'spawnShape', config: { type: 'rect', data: spec.spawn } },
      { type: 'blendMode', config: { blendMode: 'normal' } },
    ],
  }
}

function seedField(emitter) {
  const count = Math.round(emitter.maxParticles * 0.55)
  if (count < 1) return
  const wave = emitter.particlesPerWave
  emitter.particlesPerWave = count
  emitter.emitNow()
  emitter.particlesPerWave = wave
}

function applySlices(filter, clock, slices, offset) {
  const count = Math.round(slices)
  if (count < 2 || offset <= 0) return false
  if (filter.slices !== count) filter.slices = count
  filter.offset = offset
  filter.fillMode = GlitchFilter.LOOP
  filter.seed = unitSeed(clock)
  const rand = mulberry32((Math.floor(Math.max(0, clock) * 30) + 1) * 997)
  const sizes = new Float32Array(count)
  let remain = 1
  for (let i = 0; i < count - 1; i++) {
    const take = remain * (0.2 + rand() * 0.45)
    sizes[i] = take
    remain -= take
  }
  sizes[count - 1] = Math.max(0.02, remain)
  const offsets = new Float32Array(count)
  for (let i = 0; i < count; i++) offsets[i] = rand() * 2 - 1
  filter.sizes = sizes
  filter.offsets = offsets
  filter.redraw()
  return true
}

function ensure() {
  if (app) return true
  if (failed || !bufferCtx) return false
  try {
    PIXI.utils.skipHello()
    app = new PIXI.Application({
      width: W,
      height: H,
      backgroundAlpha: 0,
      antialias: false,
      resolution: 1,
      autoDensity: false,
      preserveDrawingBuffer: true,
      autoStart: false,
      clearBeforeRender: true,
    })
    app.ticker?.stop()
    stage = app.stage

    const textures = {
      flake: softCircle(64, 'rgba(255,255,255,1)', 'rgba(255,255,255,0)'),
      puff: softCircle(128, 'rgba(236,242,248,0.55)', 'rgba(236,242,248,0)'),
      streak: streakTexture(),
    }

    baseTexture = PIXI.BaseTexture.from(buffer, { scaleMode: PIXI.SCALE_MODES.LINEAR })
    sprite = new PIXI.Sprite(new PIXI.Texture(baseTexture))
    sprite.width = W
    sprite.height = H

    const wave = PIXI.BaseTexture.from(waveMap(), { scaleMode: PIXI.SCALE_MODES.LINEAR })
    wave.wrapMode = PIXI.WRAP_MODES.REPEAT
    displacementSprite = new PIXI.Sprite(new PIXI.Texture(wave))
    displacementSprite.renderable = false
    displacementSprite.width = W
    displacementSprite.height = H

    flash = new PIXI.Graphics()
    flash.beginFill(0xffffff)
    flash.drawRect(0, 0, W, H)
    flash.endFill()
    flash.visible = false

    particleHolder = new PIXI.Container()
    stage.addChild(displacementSprite, sprite, flash, particleHolder)

    grade = new PIXI.ColorMatrixFilter()
    blur = new PIXI.BlurFilter(4, 4)
    overlay = new ColorOverlayFilter(0xffffff, 0)
    godray = new GodrayFilter({ angle: 32, parallel: true, lacunarity: 2.4 })
    glitch = new GlitchFilter({ slices: 8, offset: 40, fillMode: GlitchFilter.LOOP, red: [6, 0], green: [0, 0], blue: [-6, 0] })
    rgb = new RGBSplitFilter([0, 0], [0, 0], [0, 0])
    crt = new CRTFilter({ curvature: 1, lineWidth: 2, lineContrast: 0.3, noise: 0.15, vignetting: 0.28, vignettingAlpha: 0.65 })
    film = new OldFilmFilter({ sepia: 0.12, noise: 0.2, scratch: 0.35, scratchDensity: 0.25 })
    colorMap = new ColorMapFilter(duoMap(), false, 1)
    displace = new PIXI.DisplacementFilter(displacementSprite)
    pixelate = new PixelateFilter(8)
    noise = new PIXI.NoiseFilter(0.4, 0)
    hsl = new HslAdjustmentFilter({ saturation: 0.4 })

    app._textures = textures
    return true
  } catch (err) {
    failed = true
    if (!warned) {
      warned = true
      console.warn('Overlay effects need WebGL.', err)
    }
    return false
  }
}

function configure(id, local, settings) {
  flash.visible = false
  flash.alpha = 0
  const filters = []
  const clock = local * pace(settings)

  if (id === 'storm') {
    const darkness = scaled(id, settings, 'darkness', 0.336)
    if (darkness > 0) {
      grade.reset()
      grade.brightness(1 - darkness, false)
      filters.push(grade)
    }
    flash.alpha = lightning(local, slider(settings, 'lightning') / 100, pace(settings, 'flash'))
    flash.visible = flash.alpha > 0.01
  } else if (id === 'fog') {
    const soft = scaled(id, settings, 'blur', 7.8)
    const haze = scaled(id, settings, 'haze', 0.318)
    if (soft > 0) {
      blur.blur = soft
      filters.push(blur)
    }
    if (haze > 0) {
      overlay.color = 0xe7eef6
      overlay.alpha = haze
      filters.push(overlay)
    }
  } else if (id === 'sun') {
    const rays = scaled(id, settings, 'rays', 1)
    const warmth = scaled(id, settings, 'warmth', 1)
    if (warmth > 0) {
      grade.reset()
      grade.brightness(1 + 0.11 * warmth, false)
      grade.saturate(0.325 * warmth, true)
      filters.push(grade)
    }
    if (rays > 0) {
      godray.time = clock
      godray.gain = 0.67 * rays
      godray.alpha = 0.785 * rays
      filters.push(godray)
    }
  } else if (id === 'blizzard') {
    const whiteout = scaled(id, settings, 'whiteout', 0.38)
    if (whiteout > 0) {
      overlay.color = 0xffffff
      overlay.alpha = whiteout
      filters.push(overlay)
    }
  } else if (id === 'glitch') {
    const offset = scaled(id, settings, 'offset', 79)
    const slices = slider(settings, 'slices') > 0 ? Math.max(2, Math.round(scaled(id, settings, 'slices', 12))) : 0
    if (applySlices(glitch, clock, slices, offset)) filters.push(glitch)
    const shift = scaled(id, settings, 'split', 12.8)
    if (shift > 0) {
      rgb.red = [shift, 0]
      rgb.green = [0, 0]
      rgb.blue = [-shift, shift * 0.25]
      filters.push(rgb)
    }
  } else if (id === 'duotone') {
    const mix = scaled(id, settings, 'color', 0.886)
    const wave = scaled(id, settings, 'wave', 1)
    if (mix > 0) {
      colorMap.mix = mix
      filters.push(colorMap)
    }
    if (wave > 0) {
      displace.scale.x = 72 * wave
      displace.scale.y = 33.2 * wave
      displacementSprite.x = Math.sin(clock * 0.8) * 16
      displacementSprite.y = (clock * 46) % 128
      filters.push(displace)
    }
  } else if (id === 'vhs') {
    const scan = scaled(id, settings, 'scanlines', 0.44)
    const grain = scaled(id, settings, 'noise', 1)
    const scratches = scaled(id, settings, 'scratches', 0.535)
    const curve = scaled(id, settings, 'curve', 1)
    if (scan > 0 || grain > 0 || curve > 0) {
      crt.time = clock
      crt.seed = unitSeed(clock)
      crt.noise = 0.276 * grain
      crt.lineContrast = scan
      crt.lineWidth = scan > 0 ? 2 : 0
      crt.curvature = 1.26 * curve
      crt.vignetting = 0.28 * curve
      crt.vignettingAlpha = curve > 0 ? 0.65 : 0
      filters.push(crt)
    }
    if (grain > 0 || scratches > 0 || curve > 0) {
      film.seed = unitSeed(clock + 4)
      film.noise = 0.36 * grain
      film.scratch = scratches
      film.scratchDensity = scratches > 0 ? 0.25 : 0
      film.sepia = 0.194 * curve
      filters.push(film)
    }
  } else if (id === 'signal') {
    if (slider(settings, 'blocks') > 0) {
      pixelate.size = Math.max(2, Math.round(scaled(id, settings, 'blocks', 13)))
      filters.push(pixelate)
    }
    const grain = scaled(id, settings, 'noise', 0.505)
    if (grain > 0) {
      noise.noise = grain
      noise.seed = unitSeed(clock)
      filters.push(noise)
    }
    const breakup = scaled(id, settings, 'breakup', 128)
    if (applySlices(glitch, clock + 1.7, breakup > 0 ? 4 : 0, breakup)) filters.push(glitch)
  } else if (id === 'prism') {
    const amp = scaled(id, settings, 'split', 20.8)
    const color = scaled(id, settings, 'color', 1)
    if (amp > 0) {
      rgb.red = [Math.sin(clock * 2.1) * amp, Math.cos(clock * 1.3) * amp * 0.35]
      rgb.green = [Math.sin(clock * 1.1) * amp * 0.2, Math.sin(clock * 1.7) * amp * 0.3]
      rgb.blue = [Math.cos(clock * 1.9) * amp, Math.sin(clock * 0.8) * amp * 0.25]
      filters.push(rgb)
    }
    if (color > 0) {
      hsl.hue = wrapHue(clock * 28)
      hsl.saturation = 0.61 * color
      filters.push(hsl)
    }
  }

  sprite.filters = filters.length ? filters : null
}

function ensureState(clip) {
  let state = states.get(clip.id)
  if (state && state.effect === clip.effect) return state
  state?.emitter?.destroy()
  const spec = PARTICLES[clip.effect]
  const container = new PIXI.Container()
  let emitter = null
  if (spec) {
    emitter = new Emitter(container, emitterConfig(spec, app._textures))
    emitter.autoUpdate = false
    emitter.emit = true
  }
  state = { effect: clip.effect, container, emitter, time: -1 }
  states.set(clip.id, state)
  return state
}

function stepTo(state, local, speedScale) {
  const emitter = state.emitter
  if (!emitter) return
  const advance = (dt) => {
    if (speedScale <= 0 || dt <= 1e-4) return
    emitter.update(dt * speedScale)
  }
  const jump = state.time < 0 || local + 0.001 < state.time || local - state.time > SEEK_GAP
  if (!jump) {
    advance(local - state.time)
    state.time = local
    return
  }
  emitter.cleanup()
  emitter.emit = true
  seedField(emitter)
  let clock = 0
  while (clock + STEP <= local + 1e-4) {
    advance(STEP)
    clock += STEP
  }
  advance(local - clock)
  state.time = local
}

// restyle applies size and opacity after the emitter writes absolute values.
// The authored scale is recovered when the emitter changes it, so a paused
// playhead does not shrink the particles on every redraw.
function restyle(container, settings) {
  const hasSize = Number.isFinite(settings.size)
  const hasLength = Number.isFinite(settings.length)
  const mul = hasSize ? slider(settings, 'size') / 50 : hasLength ? slider(settings, 'length') / 50 : 1
  const alphaMul = Number.isFinite(settings.opacity) ? slider(settings, 'opacity') / 100 : 1
  for (const child of container.children) {
    let authoredX = child._authoredX
    let authoredY = child._authoredY
    let authoredA = child._authoredA
    const applied = child._appliedMul
    const appliedA = child._appliedA
    if (authoredX == null || Math.abs(child.scale.x - authoredX * applied) > 1e-3) authoredX = child.scale.x
    if (authoredY == null || Math.abs(child.scale.y - authoredY * applied) > 1e-3) authoredY = child.scale.y
    if (authoredA == null || Math.abs(child.alpha - authoredA * appliedA) > 1e-3) authoredA = child.alpha
    child._authoredX = authoredX
    child._authoredY = authoredY
    child._authoredA = authoredA
    child._appliedMul = mul
    child._appliedA = alphaMul
    child.scale.set(authoredX * mul, authoredY * mul)
    child.alpha = authoredA * alphaMul
  }
}

function placeAngle(container, settings) {
  if (!Number.isFinite(settings.angle)) {
    container.rotation = 0
    container.pivot.set(0, 0)
    container.position.set(0, 0)
    return
  }
  const degrees = (settings.angle - 50) * 0.9
  container.pivot.set(W / 2, H / 2)
  container.position.set(W / 2, H / 2)
  container.rotation = (degrees * Math.PI) / 180
}

function driveParticles(state, spec, settings, local) {
  const emitter = state.emitter
  const density = slider(settings, 'density') / 100
  const speedScale = pace(settings)
  placeAngle(state.container, settings)
  if (density <= 0) {
    emitter.emit = false
    emitter.cleanup()
    state.asleep = true
    state.time = local
    return
  }
  emitter.maxParticles = Math.max(1, Math.round(spec.maxParticles * density))
  emitter.frequency = (spec.frequency * Math.max(speedScale, 0.001)) / density
  emitter.emit = true
  if (state.asleep) {
    emitter.cleanup()
    state.time = -1
    state.asleep = false
  }
  if (speedScale <= 0 && state.time >= 0) {
    if (emitter.particleCount < 1) seedField(emitter)
    state.time = local
  } else stepTo(state, local, speedScale)
  restyle(state.container, settings)
}

function upload(source) {
  bufferCtx.setTransform(1, 0, 0, 1, 0, 0)
  bufferCtx.clearRect(0, 0, W, H)
  bufferCtx.drawImage(source, 0, 0, W, H)
  baseTexture.update()
}

// syncEffects drops emitters for clips that left the project.
export function syncEffects(project) {
  if (!states.size) return
  const live = new Set()
  for (const track of project.tracks) {
    for (const clip of track.clips) if (clip.type === 'effect') live.add(clip.id)
  }
  for (const id of states.keys()) {
    if (live.has(id)) continue
    states.get(id).emitter?.destroy()
    states.delete(id)
  }
}

// resetEffects clears particle memory so an export starts from the first frame.
export function resetEffects() {
  for (const state of states.values()) state.emitter?.destroy()
  states.clear()
}

// paintEffect composites one overlay over whatever is already on ctx.
export function paintEffect(ctx, clip, t) {
  if (!ensure()) return
  const local = Math.max(0, t - clip.start)
  const settings = effectSettings(clip)
  upload(ctx.canvas)
  configure(clip.effect, local, settings)
  const state = ensureState(clip)
  particleHolder.removeChildren()
  if (state.emitter) {
    driveParticles(state, PARTICLES[clip.effect], settings, local)
    particleHolder.addChild(state.container)
  }
  app.renderer.render(stage)
  ctx.save()
  ctx.setTransform(1, 0, 0, 1, 0, 0)
  ctx.filter = 'none'
  ctx.globalAlpha = 1
  ctx.drawImage(app.view, 0, 0, W, H)
  ctx.restore()
}
