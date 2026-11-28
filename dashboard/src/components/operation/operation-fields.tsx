import { Fragment, type ReactNode } from 'react'

import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { fieldIds, type Field, type FieldValue, type Values } from '@/lib/operation'

export interface FieldProps {
  field: Field
  value: FieldValue
  error?: string
  disabled: boolean
  onChange: (v: FieldValue) => void
}

function Heading({ field, htmlFor, id }: { field: Field; htmlFor?: string; id: string }) {
  const text = (
    <>
      <span>{field.name}</span>
      <span className="text-xs font-normal text-muted-foreground">{field.cli}</span>
      <span className={field.required ? 'text-xs font-normal text-foreground' : 'text-xs font-normal text-muted-foreground'}>
        {field.required ? '(required)' : '(optional)'}
      </span>
    </>
  )
  return htmlFor ? (
    <Label id={id} htmlFor={htmlFor} className="flex-wrap leading-normal">
      {text}
    </Label>
  ) : (
    <span id={id} className="flex flex-wrap items-center gap-2 text-sm leading-normal font-medium">
      {text}
    </span>
  )
}

function Help({ id, text }: { id: string; text: string }) {
  if (!text) return null
  return (
    <p id={id} className="text-sm break-words text-muted-foreground">
      {text}
    </p>
  )
}

export function ErrorText({ id, text }: { id: string; text?: string }) {
  if (!text) return null
  return (
    <p id={id} data-slot="field-error" className="text-sm font-medium break-words text-danger">
      {text}
    </p>
  )
}

function describedBy(field: Field, error: string | undefined, i: ReturnType<typeof fieldIds>): string | undefined {
  const parts = [field.help ? i.help : '', error ? i.error : ''].filter(Boolean)
  return parts.length > 0 ? parts.join(' ') : undefined
}

// OperationField is one input of the form, built from its schema field.
function OperationField({ field, value, error, disabled, onChange }: FieldProps) {
  const i = fieldIds(field.key)
  const invalid = error ? true : undefined
  const desc = describedBy(field, error, i)

  if (field.kind === 'boolean') {
    return (
      <div data-field={field.key} className="flex flex-col gap-1.5">
        <div className="flex items-start gap-3">
          <Checkbox
            id={i.input}
            checked={value === true}
            disabled={disabled}
            aria-invalid={invalid}
            aria-describedby={desc}
            onCheckedChange={(c) => onChange(c === true)}
            className="mt-0.5"
          />
          <div className="flex min-w-0 flex-col gap-1.5">
            <Heading field={field} htmlFor={i.input} id={i.label} />
            <Help id={i.help} text={field.help} />
          </div>
        </div>
        <ErrorText id={i.error} text={error} />
      </div>
    )
  }

  let control
  if (field.kind === 'choice') {
    const common = {
      'aria-labelledby': i.label,
      'aria-describedby': desc,
      'aria-invalid': invalid,
      disabled,
      variant: 'outline' as const,
      spacing: 2,
      className: 'flex-wrap',
    }
    const items = field.allowed.map((a) => (
      <ToggleGroupItem key={a} value={a} className="min-w-16 px-3">
        {a}
      </ToggleGroupItem>
    ))
    control = field.multiple ? (
      <ToggleGroup
        type="multiple"
        value={String(value)
          .split(',')
          .filter((s) => s !== '')}
        onValueChange={(v: string[]) => onChange(v.join(','))}
        {...common}
      >
        {items}
      </ToggleGroup>
    ) : (
      <ToggleGroup type="single" value={String(value)} onValueChange={(v: string) => onChange(v)} {...common}>
        {items}
      </ToggleGroup>
    )
  } else if (field.kind === 'list') {
    control = (
      <Textarea
        id={i.input}
        value={String(value)}
        disabled={disabled}
        aria-invalid={invalid}
        aria-describedby={desc}
        rows={3}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  } else {
    control = (
      <Input
        id={i.input}
        value={String(value)}
        disabled={disabled}
        aria-invalid={invalid}
        aria-describedby={desc}
        inputMode={field.kind === 'integer' ? 'numeric' : undefined}
        autoComplete="off"
        spellCheck={false}
        onChange={(e) => onChange(e.target.value)}
      />
    )
  }
  return (
    <div data-field={field.key} className="flex flex-col gap-2">
      <Heading field={field} htmlFor={field.kind === 'choice' ? undefined : i.input} id={i.label} />
      {control}
      <Help id={i.help} text={field.help} />
      <ErrorText id={i.error} text={error} />
    </div>
  )
}

// FieldInputs lets an opener render some fields itself: it returns the
// input of a field, null to show none, or undefined for the default input.
// The value still goes through the same field and request body.
export type FieldInputs = (props: FieldProps) => ReactNode | undefined

// OperationFields is the input form of one command.
export function OperationFields({
  fields,
  values,
  errors,
  disabled,
  onChange,
  inputs,
}: {
  fields: readonly Field[]
  values: Values
  errors: Record<string, string>
  disabled: boolean
  onChange: (key: string, v: FieldValue) => void
  inputs?: FieldInputs
}) {
  if (fields.length === 0) {
    return <p className="text-sm text-muted-foreground">This command takes no input.</p>
  }
  return (
    <div className="flex flex-col gap-5">
      {fields.map((f) => {
        const props: FieldProps = {
          field: f,
          value: values[f.key] ?? f.initial,
          error: errors[f.key],
          disabled,
          onChange: (v) => onChange(f.key, v),
        }
        const custom = inputs?.(props)
        if (custom === undefined) return <OperationField key={f.key} {...props} />
        return <Fragment key={f.key}>{custom}</Fragment>
      })}
    </div>
  )
}
