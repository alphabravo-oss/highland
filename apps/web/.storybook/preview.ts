import type { Decorator, Preview } from '@storybook/react'
import '../src/index.css'

const withDarkMode: Decorator = (Story, context) => {
  const theme = (context.globals.theme as string) || 'light'
  document.documentElement.classList.toggle('dark', theme === 'dark')
  document.documentElement.dataset.theme = theme
  return Story()
}

const preview: Preview = {
  parameters: {
    backgrounds: {
      default: 'light',
      values: [
        { name: 'light', value: 'oklch(0.985 0.004 260)' },
        { name: 'dark', value: 'oklch(0.14 0.02 260)' },
      ],
    },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    layout: 'padded',
  },
  globalTypes: {
    theme: {
      description: 'Color theme (toggles .dark on <html>)',
      defaultValue: 'light',
      toolbar: {
        title: 'Theme',
        icon: 'circlehollow',
        items: [
          { value: 'light', icon: 'sun', title: 'Light' },
          { value: 'dark', icon: 'moon', title: 'Dark' },
        ],
        dynamicTitle: true,
      },
    },
  },
  decorators: [withDarkMode],
}

export default preview
