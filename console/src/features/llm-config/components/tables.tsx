import {
  AlertTriangle,
  CheckCircle2,
  CircleOff,
  Pencil,
  SlidersHorizontal,
  Trash2,
  Zap,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { LLMChannel, LLMChannelAbility, LLMRoute } from '@/features/llm-config/api/llm-config'

export function ChannelTable({
  channels,
  selectedId,
  testingId,
  onSelect,
  onEdit,
  onAbilities,
  onTest,
  onDelete,
}: {
  channels: LLMChannel[]
  selectedId: string
  testingId?: string
  onSelect: (channel: LLMChannel) => void
  onEdit: (channel: LLMChannel) => void
  onAbilities: (channel: LLMChannel) => void
  onTest: (channel: LLMChannel) => void
  onDelete: (channel: LLMChannel) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('llm_config.channels.table.name')}</TableHead>
          <TableHead>{t('llm_config.channels.table.protocol')}</TableHead>
          <TableHead>{t('llm_config.channels.table.auth')}</TableHead>
          <TableHead>{t('llm_config.channels.table.weight')}</TableHead>
          <TableHead>{t('llm_config.channels.table.status')}</TableHead>
          <TableHead className="text-right">{t('llm_config.channels.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {channels.map((channel) => (
          <TableRow
            key={channel.id}
            aria-selected={selectedId === channel.id}
            className={
              selectedId === channel.id
                ? 'cursor-pointer bg-muted/50 outline-none ring-1 ring-ring'
                : 'cursor-pointer outline-none hover:bg-muted/40 focus-visible:ring-1 focus-visible:ring-ring'
            }
            tabIndex={0}
            onClick={() => onSelect(channel)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                onSelect(channel)
              }
            }}
          >
            <TableCell>
              <div className="font-medium">{channel.name}</div>
              <div className="max-w-[22rem] truncate font-mono text-xs text-muted-foreground">
                {channel.baseUrl || 'default'}
              </div>
            </TableCell>
            <TableCell className="font-mono text-xs">{channel.protocol}</TableCell>
            <TableCell>
              <div className="flex items-center gap-1.5 text-xs">
                {channel.authMode === 'none' ? (
                  <CircleOff className="size-3.5 text-muted-foreground" />
                ) : channel.hasApiKey ? (
                  <CheckCircle2 className="size-3.5 text-green-600" />
                ) : (
                  <AlertTriangle className="size-3.5 text-amber-600" />
                )}
                {channel.authMode === 'none'
                  ? t('llm_config.channels.credential.none')
                  : channel.hasApiKey
                    ? t('llm_config.channels.credential.set')
                    : t('llm_config.channels.credential.missing')}
              </div>
            </TableCell>
            <TableCell className="text-sm">
              {channel.priority} / {channel.weight}
            </TableCell>
            <TableCell>
              <StatusText value={channel.status} />
              {channel.lastTestStatus && (
                <div className="text-[11px] text-muted-foreground">{channel.lastTestStatus}</div>
              )}
            </TableCell>
            <TableCell className="text-right" onClick={(event) => event.stopPropagation()}>
              <Button
                variant="ghost"
                size="sm"
                title={t('llm_config.actions.test')}
                onClick={() => onTest(channel)}
                disabled={testingId === channel.id}
              >
                <Zap className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                title={t('llm_config.actions.abilities')}
                onClick={() => onAbilities(channel)}
              >
                <SlidersHorizontal className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                title={t('common.edit')}
                onClick={() => onEdit(channel)}
              >
                <Pencil className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                title={t('common.delete')}
                onClick={() => onDelete(channel)}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

export function AbilityTable({
  abilities,
  onEdit,
  onDelete,
}: {
  abilities: LLMChannelAbility[]
  onEdit: (ability: LLMChannelAbility) => void
  onDelete: (ability: LLMChannelAbility) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('llm_config.abilities.table.logical')}</TableHead>
          <TableHead>{t('llm_config.abilities.table.provider')}</TableHead>
          <TableHead>{t('llm_config.abilities.table.weight')}</TableHead>
          <TableHead>{t('llm_config.abilities.table.status')}</TableHead>
          <TableHead className="text-right">{t('llm_config.channels.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {abilities.map((ability) => (
          <TableRow key={ability.id}>
            <TableCell className="font-mono text-xs">{ability.logicalModel}</TableCell>
            <TableCell className="font-mono text-xs">{ability.providerModel}</TableCell>
            <TableCell>
              {ability.priority} / {ability.weight}
            </TableCell>
            <TableCell>
              <StatusText value={ability.enabled ? 'enabled' : 'disabled'} />
            </TableCell>
            <TableCell className="text-right">
              <Button
                variant="ghost"
                size="sm"
                title={t('common.edit')}
                onClick={() => onEdit(ability)}
              >
                <Pencil className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                title={t('common.delete')}
                onClick={() => onDelete(ability)}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

export function RouteTable({
  routes,
  onEdit,
  onDelete,
}: {
  routes: LLMRoute[]
  onEdit: (route: LLMRoute) => void
  onDelete: (route: LLMRoute) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('llm_config.routes.table.scope')}</TableHead>
          <TableHead>{t('llm_config.routes.table.purpose')}</TableHead>
          <TableHead>{t('llm_config.routes.table.logical')}</TableHead>
          <TableHead>{t('llm_config.routes.table.status')}</TableHead>
          <TableHead className="text-right">{t('llm_config.channels.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {routes.map((route) => (
          <TableRow key={`${route.tenantId}:${route.purpose}`}>
            <TableCell className="font-mono text-xs">
              {route.tenantId || t('llm_config.routes.global')}
            </TableCell>
            <TableCell>{route.purpose}</TableCell>
            <TableCell className="font-mono text-xs">{route.logicalModel}</TableCell>
            <TableCell>
              <StatusText value={route.enabled ? 'enabled' : 'disabled'} />
            </TableCell>
            <TableCell className="text-right">
              <Button
                variant="ghost"
                size="sm"
                title={t('common.edit')}
                onClick={() => onEdit(route)}
              >
                <Pencil className="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                title={t('common.delete')}
                onClick={() => onDelete(route)}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function StatusText({ value }: { value: string }) {
  const color =
    value === 'enabled' || value === 'ok'
      ? 'text-green-600'
      : value === 'disabled'
        ? 'text-muted-foreground'
        : 'text-amber-600'
  return <span className={color}>{value}</span>
}
