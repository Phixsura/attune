import { Plus, Settings2, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

export interface CustomFieldDefinition {
  id: string
  fieldKey: string
  displayName: string
  fieldType: 'text' | 'number' | 'boolean' | 'enum'
  enumValues: string[]
  required: boolean
  sortOrder: number
}

const FIELD_TYPES = ['text', 'number', 'boolean', 'enum'] as const

export function CustomFieldsPage({
  fields,
  onAdd,
  onRemove,
}: {
  fields: CustomFieldDefinition[]
  onAdd: (field: Omit<CustomFieldDefinition, 'id' | 'sortOrder'>) => void
  onRemove: (id: string) => void
}) {
  const { t } = useTranslation()
  const [showForm, setShowForm] = useState(false)
  const [key, setKey] = useState('')
  const [display, setDisplay] = useState('')
  const [type, setType] = useState<CustomFieldDefinition['fieldType']>('text')
  const [enumVals, setEnumVals] = useState('')
  const [required, setRequired] = useState(false)

  const handleAdd = () => {
    if (!key.trim() || !display.trim()) return
    onAdd({
      fieldKey: key.trim(),
      displayName: display.trim(),
      fieldType: type,
      enumValues:
        type === 'enum'
          ? enumVals
              .split(',')
              .map((v) => v.trim())
              .filter(Boolean)
          : [],
      required,
    })
    setKey('')
    setDisplay('')
    setType('text')
    setEnumVals('')
    setRequired(false)
    setShowForm(false)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Settings2 className="size-5 text-muted-foreground" />
          <h2 className="text-lg font-semibold">{t('custom_fields.title')}</h2>
        </div>
        <Button size="sm" onClick={() => setShowForm(!showForm)}>
          <Plus className="size-4" />
          {t('custom_fields.add')}
        </Button>
      </div>

      {showForm && (
        <div className="rounded-md border p-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Input
              placeholder={t('custom_fields.field_key')}
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
            <Input
              placeholder={t('custom_fields.display_name')}
              value={display}
              onChange={(e) => setDisplay(e.target.value)}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Select
              value={type}
              onValueChange={(v) => setType(v as CustomFieldDefinition['fieldType'])}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {FIELD_TYPES.map((ft) => (
                  <SelectItem key={ft} value={ft}>
                    {t(`custom_fields.type_${ft}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {type === 'enum' && (
              <Input
                placeholder={t('custom_fields.enum_values_hint')}
                value={enumVals}
                onChange={(e) => setEnumVals(e.target.value)}
              />
            )}
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={required}
              onChange={(e) => setRequired(e.target.checked)}
            />
            {t('custom_fields.required')}
          </label>
          <Button size="sm" onClick={handleAdd} disabled={!key.trim() || !display.trim()}>
            {t('custom_fields.save')}
          </Button>
        </div>
      )}

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('custom_fields.field_key')}</TableHead>
            <TableHead>{t('custom_fields.display_name')}</TableHead>
            <TableHead>{t('custom_fields.type')}</TableHead>
            <TableHead>{t('custom_fields.required')}</TableHead>
            <TableHead className="w-16" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {fields.length === 0 ? (
            <TableRow>
              <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                {t('custom_fields.empty')}
              </TableCell>
            </TableRow>
          ) : (
            fields.map((f) => (
              <TableRow key={f.id}>
                <TableCell className="font-mono text-sm">{f.fieldKey}</TableCell>
                <TableCell>{f.displayName}</TableCell>
                <TableCell>
                  {t(`custom_fields.type_${f.fieldType}`)}
                  {f.fieldType === 'enum' && f.enumValues.length > 0 && (
                    <span className="ml-1 text-xs text-muted-foreground">
                      ({f.enumValues.join(', ')})
                    </span>
                  )}
                </TableCell>
                <TableCell>{f.required ? '✓' : '—'}</TableCell>
                <TableCell>
                  <Button variant="ghost" size="icon" onClick={() => onRemove(f.id)}>
                    <Trash2 className="size-4 text-destructive" />
                  </Button>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  )
}
