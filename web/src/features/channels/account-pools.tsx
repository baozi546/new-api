import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useId, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'

import { Dialog } from '@/components/dialog'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { ProviderPoolPanel } from './components/provider-pool-panel'
import {
  createPoolChannel,
  listPoolChannels,
  poolQueryKeys,
} from './lib/provider-pool-api'

const createSchema = z.object({
  plugin: z.string().min(1),
  name: z.string().trim().min(1).max(128),
})

function CreatePoolChannel(props: {
  plugins: { key: string; name: string }[]
  onCreated: (id: number) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const id = useId()
  const form = useForm<z.infer<typeof createSchema>>({
    resolver: zodResolver(createSchema),
    defaultValues: {
      plugin: props.plugins[0]?.key || '',
      name: '',
    },
  })
  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof createSchema>) =>
      createPoolChannel({
        plugin: values.plugin,
        name: values.name,
        settings: { concurrency: 1 },
      }),
    onSuccess: (data) => {
      setOpen(false)
      form.reset()
      props.onCreated(data.id)
    },
  })
  return (
    <>
      <Button disabled={!props.plugins.length} onClick={() => setOpen(true)}>
        {t('Create account pool channel')}
      </Button>
      <Dialog
        open={open}
        onOpenChange={setOpen}
        title={t('Create account pool channel')}
        footer={
          <Button
            type='submit'
            form={`${id}-form`}
            disabled={mutation.isPending}
          >
            {t('Create')}
          </Button>
        }
      >
        <form
          id={`${id}-form`}
          onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
        >
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor={`${id}-plugin`}>{t('Provider')}</FieldLabel>
              <Select
                value={form.watch('plugin')}
                items={props.plugins.map((plugin) => ({
                  value: plugin.key,
                  label: plugin.name,
                }))}
                onValueChange={(value) => form.setValue('plugin', value || '')}
              >
                <SelectTrigger id={`${id}-plugin`} className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {props.plugins.map((plugin) => (
                    <SelectItem key={plugin.key} value={plugin.key}>
                      {plugin.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor={`${id}-name`}>
                {t('Channel name')}
              </FieldLabel>
              <Input
                id={`${id}-name`}
                {...form.register('name')}
                aria-invalid={!!form.formState.errors.name}
              />
              {form.formState.errors.name && (
                <FieldError>{t('Required')}</FieldError>
              )}
            </Field>
            <FieldDescription>
              {t(
                'Models and Plus/Pro account matching are detected automatically after import.'
              )}
            </FieldDescription>
          </FieldGroup>
        </form>
      </Dialog>
    </>
  )
}

export function AccountPools() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [selected, setSelected] = useState<number | null>(null)
  const query = useQuery({
    queryKey: poolQueryKeys.channels,
    queryFn: listPoolChannels,
  })
  const channels = query.data?.channels || []
  const current =
    channels.find((channel) => channel.id === selected) || channels[0]
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Account pools')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {query.data && (
          <CreatePoolChannel
            plugins={query.data.plugins}
            onCreated={(id) => {
              setSelected(id)
              void queryClient.invalidateQueries({
                queryKey: poolQueryKeys.channels,
              })
            }}
          />
        )}
        <Link to='/channels' className={buttonVariants({ variant: 'outline' })}>
          {t('Channels')}
        </Link>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        {query.isPending && <LoadingState />}
        {query.isError && (
          <ErrorState
            title={t('Failed to load accounts')}
            onRetry={() => void query.refetch()}
          />
        )}
        {query.isSuccess && !channels.length && (
          <EmptyState
            title={t('Create a channel to start managing account pools')}
          />
        )}
        {current && (
          <div className='space-y-4'>
            <Field>
              <FieldLabel>
                {t('Target channel for account assignment')}
              </FieldLabel>
              <Select
                value={String(current.id)}
                items={channels.map((channel) => ({
                  value: String(channel.id),
                  label: `${channel.name} (${channel.groups.join(', ')})`,
                }))}
                onValueChange={(value) => setSelected(Number(value))}
              >
                <SelectTrigger
                  className='w-full sm:w-96'
                  aria-label={t('Target channel for account assignment')}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {channels.map((channel) => (
                    <SelectItem key={channel.id} value={String(channel.id)}>
                      {channel.name} ({channel.groups.join(', ')})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <ProviderPoolPanel
              key={current.id}
              channelId={current.id}
              channelName={current.name}
              channels={channels}
              plugin={current.plugin}
            />
          </div>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
