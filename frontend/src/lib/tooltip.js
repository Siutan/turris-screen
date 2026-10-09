import tippy from 'tippy.js'
import 'tippy.js/dist/tippy.css'

// Icon labels use Tippy so the browser's native title tooltip never appears.
export function tooltip(node, content) {
  const instance = tippy(node, {
    content: content ?? '',
    placement: 'right',
    offset: [0, 10],
    delay: [280, 0],
    duration: [140, 90],
    appendTo: () => document.body,
  })
  return {
    update(next) {
      instance.setContent(next ?? '')
    },
    destroy() {
      instance.destroy()
    },
  }
}
