import { createFileRoute } from '@tanstack/react-router'

import { ChannelInspections } from '@/features/channels/channel-inspections'

export const Route = createFileRoute('/_authenticated/channel-inspections')({
  component: ChannelInspections,
})
