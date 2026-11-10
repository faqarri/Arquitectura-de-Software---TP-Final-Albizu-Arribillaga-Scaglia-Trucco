import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// jsdom no implementa el desplazamiento de la ventana.
window.scrollTo = () => {}

afterEach(() => {
  cleanup()
  sessionStorage.clear()
  localStorage.clear()
})
