import * as React from 'react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { Check, ChevronsUpDown, Loader2, X } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface ComboOption {
  value: string
  label?: string
  hint?: React.ReactNode
  icon?: React.ReactNode
}

interface ComboboxProps {
  value: string
  onChange: (value: string) => void
  options: ComboOption[]
  placeholder?: string
  loading?: boolean
  /** Allow free text that is not one of the options (default true). */
  allowCustom?: boolean
  emptyText?: string
  icon?: React.ReactNode
  required?: boolean
  disabled?: boolean
  clearable?: boolean
  className?: string
  id?: string
}

/**
 * Autocomplete input. Focusing it shows every option (the current text is
 * selected so typing replaces it); typing filters the list.
 */
export function Combobox({
  value,
  onChange,
  options,
  placeholder,
  loading,
  allowCustom = true,
  emptyText = 'No matches',
  icon,
  required,
  disabled,
  clearable = true,
  className,
  id,
}: ComboboxProps) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState<string | null>(null) // null: not filtering
  const [active, setActive] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const listId = useId()

  const selected = options.find((o) => o.value === value)
  const display = query ?? (selected?.label ?? value)

  const filtered = useMemo(() => {
    if (!query) return options
    const q = query.toLowerCase()
    return options.filter((o) => (o.label ?? o.value).toLowerCase().includes(q) || o.value.toLowerCase().includes(q))
  }, [options, query])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) close()
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  useEffect(() => {
    listRef.current?.querySelector<HTMLElement>(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active])

  const close = () => {
    setOpen(false)
    setQuery(null)
  }

  const openList = () => {
    if (disabled) return
    setOpen(true)
    setQuery(null)
    setActive(Math.max(0, options.findIndex((o) => o.value === value)))
  }

  const pick = (o: ComboOption) => {
    onChange(o.value)
    close()
    inputRef.current?.blur()
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      if (!open) return openList()
      const d = e.key === 'ArrowDown' ? 1 : -1
      setActive((a) => (filtered.length ? (a + d + filtered.length) % filtered.length : 0))
    } else if (e.key === 'Enter') {
      if (open && filtered[active]) {
        e.preventDefault()
        pick(filtered[active])
      } else if (open) {
        e.preventDefault()
        close()
      }
    } else if (e.key === 'Escape') {
      if (open) {
        e.preventDefault()
        e.stopPropagation()
        close()
      }
    } else if (e.key === 'Tab') {
      close()
    }
  }

  return (
    <div ref={rootRef} className={cn('relative', className)}>
      <div
        className={cn(
          'flex h-9 w-full items-center gap-2 rounded-lg border border-input bg-card px-3 shadow-xs transition-colors focus-within:ring-2 focus-within:ring-ring',
          disabled && 'cursor-not-allowed opacity-50',
        )}
      >
        {(selected?.icon ?? icon) && <span className="shrink-0 text-muted-foreground [&_svg]:size-4">{selected?.icon ?? icon}</span>}
        <input
          ref={inputRef}
          id={id}
          role="combobox"
          aria-expanded={open}
          aria-controls={listId}
          aria-autocomplete="list"
          autoComplete="off"
          spellCheck={false}
          className="h-full min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground disabled:cursor-not-allowed"
          value={display}
          placeholder={placeholder}
          required={required}
          disabled={disabled}
          onFocus={(e) => {
            openList()
            e.currentTarget.select()
          }}
          onClick={() => !open && openList()}
          onChange={(e) => {
            const v = e.target.value
            setQuery(v)
            setOpen(true)
            setActive(0)
            if (allowCustom) onChange(v)
          }}
          onBlur={() => {
            // Without free text, revert to the last valid choice.
            if (!allowCustom) setQuery(null)
          }}
          onKeyDown={onKeyDown}
        />
        {loading && <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />}
        {clearable && value && !disabled && allowCustom && (
          <button
            type="button"
            tabIndex={-1}
            className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => {
              onChange('')
              setQuery('')
              inputRef.current?.focus()
            }}
            title="Clear"
          >
            <X className="size-3.5" />
          </button>
        )}
        <button
          type="button"
          tabIndex={-1}
          disabled={disabled}
          className="shrink-0 text-muted-foreground hover:text-foreground"
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => (open ? close() : (inputRef.current?.focus(), openList()))}
        >
          <ChevronsUpDown className="size-4" />
        </button>
      </div>

      {open && (
        <ul
          ref={listRef}
          id={listId}
          role="listbox"
          className="absolute z-50 mt-1 max-h-64 w-full overflow-auto rounded-lg border bg-card p-1 shadow-lg"
        >
          {filtered.map((o, i) => (
            <li
              key={o.value}
              data-index={i}
              role="option"
              aria-selected={o.value === value}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => pick(o)}
              onMouseEnter={() => setActive(i)}
              className={cn(
                'flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm',
                i === active && 'bg-muted',
              )}
            >
              {o.icon && <span className="shrink-0 text-muted-foreground [&_svg]:size-4">{o.icon}</span>}
              <span className="min-w-0 flex-1 truncate">{o.label ?? o.value}</span>
              {o.hint && <span className="shrink-0 text-xs text-muted-foreground">{o.hint}</span>}
              <Check className={cn('size-4 shrink-0 text-primary', o.value === value ? 'opacity-100' : 'opacity-0')} />
            </li>
          ))}
          {!filtered.length && (
            <li className="px-2 py-2 text-sm text-muted-foreground">
              {loading ? 'Loading…' : allowCustom && query ? `Use “${query}”` : emptyText}
            </li>
          )}
        </ul>
      )}
    </div>
  )
}
