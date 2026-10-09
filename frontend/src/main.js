import { mount } from 'svelte'
import './style.css'
import './studio.css'
import App from './App.svelte'
import { applyTheme, storedTheme } from './lib/theme.js'

applyTheme(storedTheme())

const app = mount(App, {
  target: document.getElementById('app'),
})

export default app
