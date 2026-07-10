import type { Meta, StoryObj } from '@storybook/react'
import { Input } from './input'
import { Label } from './label'

const meta = {
  title: 'UI/Input',
  component: Input,
  tags: ['autodocs'],
  args: {
    placeholder: 'Enter value…',
  },
  argTypes: {
    disabled: { control: 'boolean' },
    type: {
      control: 'select',
      options: ['text', 'password', 'email', 'number', 'search'],
    },
  },
} satisfies Meta<typeof Input>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {}

export const Password: Story = {
  args: {
    type: 'password',
    placeholder: 'Password',
    defaultValue: 'secret',
  },
}

export const Disabled: Story = {
  args: {
    disabled: true,
    defaultValue: 'Read only value',
  },
}

export const WithLabel: Story = {
  render: (args) => (
    <div className="w-72 space-y-1.5">
      <Label htmlFor="story-input">Username</Label>
      <Input id="story-input" {...args} />
    </div>
  ),
  args: {
    placeholder: 'admin',
  },
}
