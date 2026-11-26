import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Layers, Plus } from 'lucide-react'
import { useId, useMemo, useState } from 'react'
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
import { resolveLocalizedText } from '@/lib/localized-text'
import { cn } from '@/lib/utils'

import { ProviderPoolPanel } from './components/provider-pool-panel'
import {
  createPoolChannel,
  listPoolChannels,
  poolQueryKeys,
  type PoolPlugin,
} from './lib/provider-pool-api'
import { getProviderVisual } from './lib/provider-pool-meta'

const createSchema = z.object({
  plugin: z.string().min(1),
  name: z.string().trim().min(1).max(128),
  groups: z.string().optional(),
})

function CreatePoolChannel(props: {
  plugins: PoolPlugin[]
  onCreated: (id: number) => void
}) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const id = useId()
  const form = useForm<z.infer<typeof createSchema>>({
    resolver: zodResolver(createSchema),
    defaultValues: {
      plugin: props.plugins[0]?.key || '',
      name: '',
      groups: '',
    },
  })
  const selectedPlugin = form.watch('plugin')
  const isGpt = selectedPlugin === 'gpt-account-pool'

  const mutation = useMutation({
    mutationFn: (values: z.infer<typeof createSchema>) => {
      const parsedGroups = (values.groups || '')
        .split(/[,，\s]+/)
        .map((g) => g.trim())
        .filter(Boolean)
      return createPoolChannel({
        plugin: values.plugin,
        name: values.name,
        groups: parsedGroups.length > 0 ? parsedGroups : undefined,
        settings: { concurrency: 1 },
      })
    },
    onSuccess: (data) => {
      setOpen(false)
      form.reset()
      props.onCreated(data.id)
    },
  })
  return (
    <>
      <Button disabled={!props.plugins.length} onClick={() => setOpen(true)}>
        <Plus className='size-4' />
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
                value={selectedPlugin}
                items={props.plugins.map((plugin) => ({
                  value: plugin.key,
                  label:
                    resolveLocalizedText(plugin.name, i18n.language) ||
                    plugin.key,
                }))}
                onValueChange={(value) => form.setValue('plugin', value || '')}
              >
                <SelectTrigger id={`${id}-plugin`} className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {props.plugins.map((plugin) => (
                    <SelectItem key={plugin.key} value={plugin.key}>
                      {resolveLocalizedText(plugin.name, i18n.language) ||
                        plugin.key}
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
            <Field>
              <FieldLabel htmlFor={`${id}-groups`}>
                {t('Customer groups (optional)')}
              </FieldLabel>
              <Input
                id={`${id}-groups`}
                placeholder={isGpt ? 'plus, pro' : 'default'}
                {...form.register('groups')}
              />
              <FieldDescription>
                {t(
                  isGpt
                    ? 'Models and Plus/Pro account matching are detected automatically after import.'
                    : 'Leave blank to use the provider default group. Accounts and models sync after login or import.'
                )}
              </FieldDescription>
            </Field>
          </FieldGroup>
        </form>
      </Dialog>
    </>
  )
}

export function AccountPools() {
  const { t, i18n } = useTranslation()
  const queryClient = useQueryClient()
  const [providerFilter, setProviderFilter] = useState<string>('all')
  // null means CPA-style "All accounts" unified view across channels
  const [selectedChannelId, setSelectedChannelId] = useState<number | null>(
    null
  )

  const query = useQuery({
    queryKey: poolQueryKeys.channels,
    queryFn: listPoolChannels,
  })

  const channels = query.data?.channels || []
  const plugins = query.data?.plugins || []

  const providerTabs = useMemo(() => {
    const map = new Map<
      string,
      { key: string; name: string; channelCount: number; accountCount: number }
    >()
    for (const plugin of plugins) {
      map.set(plugin.key, {
        key: plugin.key,
        name: resolveLocalizedText(plugin.name, i18n.language) || plugin.key,
        channelCount: 0,
        accountCount: 0,
      })
    }
    for (const ch of channels) {
      const existing = map.get(ch.plugin.key) || {
        key: ch.plugin.key,
        name:
          resolveLocalizedText(ch.plugin.name, i18n.language) || ch.plugin.key,
        channelCount: 0,
        accountCount: 0,
      }
      existing.channelCount += 1
      existing.accountCount += ch.account_count ?? 0
      map.set(ch.plugin.key, existing)
    }
    return Array.from(map.values())
  }, [channels, plugins, i18n.language])

  const filteredChannels = useMemo(
    () =>
      providerFilter === 'all'
        ? channels
        : channels.filter((ch) => ch.plugin.key === providerFilter),
    [channels, providerFilter]
  )

  const currentChannel =
    selectedChannelId !== null
      ? filteredChannels.find((ch) => ch.id === selectedChannelId) || null
      : null

  const totalAccountsInScope = filteredChannels.reduce(
    (sum, ch) => sum + (ch.account_count ?? 0),
    0
  )

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Account pools')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {query.data && (
          <CreatePoolChannel
            plugins={query.data.plugins}
            onCreated={(id) => {
              setSelectedChannelId(id)
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
        {channels.length > 0 && (
          <div className='space-y-4'>
            {/* CPA-style Multi-Provider & Channel Tab Bar */}
            <div className='space-y-2.5'>
              {providerTabs.length > 1 && (
                <div className='flex flex-wrap items-center gap-1.5'>
                  <button
                    type='button'
                    onClick={() => {
                      setProviderFilter('all')
                      setSelectedChannelId(null)
                    }}
                    className={cn(
                      'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors',
                      providerFilter === 'all'
                        ? 'border-primary bg-primary text-primary-foreground'
                        : 'border-border bg-card hover:bg-muted'
                    )}
                  >
                    <Layers className='size-3.5' />
                    {t('All providers')}
                  </button>
                  {providerTabs.map((prov) => {
                    const visual = getProviderVisual(prov.key)
                    const active = providerFilter === prov.key
                    return (
                      <button
                        key={prov.key}
                        type='button'
                        onClick={() => {
                          setProviderFilter(prov.key)
                          setSelectedChannelId(null)
                        }}
                        className={cn(
                          'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors',
                          active
                            ? 'border-primary bg-primary text-primary-foreground'
                            : 'border-border bg-card hover:bg-muted'
                        )}
                      >
                        <span
                          className={cn(
                            'size-2 rounded-full',
                            active ? 'bg-primary-foreground' : visual.dotClass
                          )}
                        />
                        {prov.name}
                        <span className='opacity-75'>
                          ({prov.accountCount})
                        </span>
                      </button>
                    )
                  })}
                </div>
              )}

              {/* Channel switcher pills: All pools + individual channels */}
              <div className='flex flex-wrap items-center gap-2'>
                <button
                  type='button'
                  onClick={() => setSelectedChannelId(null)}
                  aria-pressed={currentChannel === null}
                  className={cn(
                    'inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-xs font-medium transition-all',
                    currentChannel === null
                      ? 'border-primary bg-primary/10 text-primary ring-primary/30 ring-1'
                      : 'border-border bg-card text-foreground/80 hover:bg-muted'
                  )}
                >
                  <Layers className='size-3.5' />
                  <span>{t('All pools')}</span>
                  <span className='bg-background/80 rounded-full px-1.5 py-px font-mono text-[11px]'>
                    {totalAccountsInScope}
                  </span>
                </button>

                {filteredChannels.map((channel) => {
                  const visual = getProviderVisual(channel.plugin.key)
                  const active = currentChannel?.id === channel.id
                  const problemCount = channel.problem_count ?? 0
                  return (
                    <button
                      key={channel.id}
                      type='button'
                      onClick={() => setSelectedChannelId(channel.id)}
                      aria-pressed={active}
                      className={cn(
                        'inline-flex items-center gap-2 rounded-lg border px-3 py-1.5 text-xs font-medium transition-all',
                        active
                          ? 'border-primary bg-primary/10 text-primary ring-primary/30 ring-1'
                          : 'border-border bg-card text-foreground/80 hover:bg-muted'
                      )}
                    >
                      <span
                        className={cn('size-2 rounded-full', visual.dotClass)}
                      />
                      <span>{channel.name}</span>
                      {channel.groups.length > 0 && (
                        <span className='text-muted-foreground text-[10px]'>
                          ({channel.groups.join(', ')})
                        </span>
                      )}
                      <span className='bg-background/80 rounded-full px-1.5 py-px font-mono text-[11px]'>
                        {channel.account_count ?? 0}
                      </span>
                      {problemCount > 0 && (
                        <span className='rounded-full bg-red-500/15 px-1.5 py-px font-mono text-[10px] font-semibold text-red-600 dark:text-red-400'>
                          {problemCount}
                        </span>
                      )}
                    </button>
                  )
                })}
              </div>
            </div>

            <ProviderPoolPanel
              key={
                currentChannel
                  ? `ch-${currentChannel.id}`
                  : `all-${providerFilter}`
              }
              viewAll={currentChannel === null}
              pluginFilter={
                providerFilter === 'all' ? undefined : providerFilter
              }
              channelId={currentChannel?.id ?? null}
              channelName={currentChannel?.name || ''}
              channels={channels}
              plugin={currentChannel?.plugin}
            />
          </div>
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
