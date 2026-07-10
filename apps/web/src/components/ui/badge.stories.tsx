import type { Meta, StoryObj } from '@storybook/react'
import { Badge } from './badge'

const meta = {
  title: 'UI/Badge',
  component: Badge,
  tags: ['autodocs'],
  args: {
    children: 'Badge',
  },
  argTypes: {
    tone: {
      control: 'select',
      options: ['default', 'success', 'warning', 'danger', 'info', 'primary'],
    },
  },
} satisfies Meta<typeof Badge>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {}

export const Success: Story = {
  args: { tone: 'success', children: 'Healthy' },
}

export const Warning: Story = {
  args: { tone: 'warning', children: 'Degraded' },
}

export const Danger: Story = {
  args: { tone: 'danger', children: 'Faulted' },
}

export const Info: Story = {
  args: { tone: 'info', children: 'Info' },
}

export const Primary: Story = {
  args: { tone: 'primary', children: 'Primary' },
}

export const AllTones: Story = {
  render: () => (
    <div className="flex flex-wrap items-center gap-2">
      <Badge tone="default">Default</Badge>
      <Badge tone="success">Success</Badge>
      <Badge tone="warning">Warning</Badge>
      <Badge tone="danger">Danger</Badge>
      <Badge tone="info">Info</Badge>
      <Badge tone="primary">Primary</Badge>
    </div>
  ),
}
