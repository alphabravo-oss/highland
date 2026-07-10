import type { Meta, StoryObj } from '@storybook/react'
import { PageHeader } from './PageHeader'
import { Button } from '@/components/ui/button'

const meta = {
  title: 'Data/PageHeader',
  component: PageHeader,
  tags: ['autodocs'],
  args: {
    title: 'Volumes',
    description: 'Manage Longhorn volumes, attachments, and snapshots.',
  },
} satisfies Meta<typeof PageHeader>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {}

export const TitleOnly: Story = {
  args: {
    title: 'Dashboard',
    description: undefined,
  },
}

export const WithActions: Story = {
  args: {
    title: 'Volumes',
    description: 'Create and operate cluster storage volumes.',
    actions: (
      <>
        <Button variant="outline">Export</Button>
        <Button>Create volume</Button>
      </>
    ),
  },
}
