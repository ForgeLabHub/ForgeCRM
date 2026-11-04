/**
 * Form controls for the ForgeCRM design system.
 *
 * One implementation per control, so a button on the Contacts page and a button
 * in an admin dialog cannot drift apart. Every control takes its look from the
 * tokens in index.css; the variants here only choose which tokens apply.
 *
 * The native element is kept underneath (input, select, button) rather than
 * rebuilt, so keyboard behaviour, form submission and assistive-technology
 * support come from the platform instead of being re-implemented and got wrong.
 */
import { forwardRef, useId } from 'react'
import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
} from 'react'
import { IconClose, IconSearch } from '../icons'

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'danger-ghost'
type ButtonSize = 'sm' | 'md' | 'lg'

const VARIANT_CLASS: Record<ButtonVariant, string> = {
  primary: 'btn--primary',
  secondary: 'btn--secondary',
  ghost: 'btn--ghost',
  danger: 'btn--danger',
  'danger-ghost': 'btn--danger-ghost',
}

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  /** Shows a spinner and blocks further clicks while a request is in flight. */
  loading?: boolean
  /** Leading icon. Replaced by the spinner while `loading`. */
  icon?: ReactNode
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading = false, icon, children, className, disabled, ...rest },
  ref,
) {
  const classes = [
    'btn',
    VARIANT_CLASS[variant],
    size !== 'md' ? `btn--${size}` : '',
    className ?? '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <button
      ref={ref}
      type={rest.type ?? 'button'}
      className={classes}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      {...rest}
    >
      {loading ? <span className="btn__spinner" aria-hidden="true" /> : icon}
      {children}
    </button>
  )
})

export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  /** Required: an icon-only control has no visible text for a screen reader. */
  label: string
  variant?: ButtonVariant
  size?: ButtonSize
  children: ReactNode
}

export function IconButton({
  label,
  variant = 'ghost',
  size = 'md',
  children,
  className,
  ...rest
}: IconButtonProps) {
  const classes = [
    'btn',
    'btn--icon',
    VARIANT_CLASS[variant],
    size !== 'md' ? `btn--${size}` : '',
    className ?? '',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <button
      type={rest.type ?? 'button'}
      className={classes}
      aria-label={label}
      title={label}
      {...rest}
    >
      {children}
    </button>
  )
}

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  invalid?: boolean
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { invalid = false, className, ...rest },
  ref,
) {
  return (
    <input
      ref={ref}
      className={['input', invalid ? 'input--invalid' : '', className ?? ''].filter(Boolean).join(' ')}
      aria-invalid={invalid || undefined}
      {...rest}
    />
  )
})

export interface SearchInputProps extends InputHTMLAttributes<HTMLInputElement> {
  onClear?: () => void
}

/** A text input with a leading search glyph and an optional clear affordance. */
export const SearchInput = forwardRef<HTMLInputElement, SearchInputProps>(function SearchInput(
  { onClear, value, className, ...rest },
  ref,
) {
  const hasValue = typeof value === 'string' && value.length > 0
  return (
    <div className={['search-input', className ?? ''].filter(Boolean).join(' ')}>
      <IconSearch size={15} />
      <input ref={ref} type="search" className="input" value={value} {...rest} />
      {hasValue && onClear ? (
        <button
          type="button"
          className="search-input__clear"
          aria-label="Clear search"
          onClick={onClear}
        >
          <IconClose size={14} />
        </button>
      ) : null}
    </div>
  )
})

export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  invalid?: boolean
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { invalid = false, className, children, ...rest },
  ref,
) {
  return (
    <select
      ref={ref}
      className={['select', invalid ? 'select--invalid' : '', className ?? ''].filter(Boolean).join(' ')}
      aria-invalid={invalid || undefined}
      {...rest}
    >
      {children}
    </select>
  )
})

export interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label?: ReactNode
}

export function Checkbox({ label, id, ...rest }: CheckboxProps) {
  const generated = useId()
  const inputId = id ?? generated
  return (
    <label className="checkbox" htmlFor={inputId}>
      <input id={inputId} type="checkbox" {...rest} />
      {label ? <span>{label}</span> : null}
    </label>
  )
}

export interface SwitchProps {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  id?: string
  disabled?: boolean
}

/** An accessible on/off control: a real checkbox with a styled track. */
export function Switch({ checked, onChange, label, id, disabled }: SwitchProps) {
  const generated = useId()
  const inputId = id ?? generated
  return (
    <label className="switch" htmlFor={inputId}>
      <input
        id={inputId}
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="switch__track" aria-hidden="true">
        <span className="switch__thumb" />
      </span>
      <span>{label}</span>
    </label>
  )
}

export interface FormFieldProps {
  label: string
  htmlFor?: string
  error?: string
  hint?: string
  required?: boolean
  children: ReactNode
  full?: boolean
}

/** A labelled control with room for a hint and an inline error message. */
export function FormField({
  label,
  htmlFor,
  error,
  hint,
  required,
  children,
  full = false,
}: FormFieldProps) {
  return (
    <div className={full ? 'field form-grid__full' : 'field'}>
      <label className="field__label" htmlFor={htmlFor}>
        {label}
        {required ? (
          <span className="field__required" aria-hidden="true">
            {' '}
            *
          </span>
        ) : null}
      </label>
      {children}
      {hint && !error ? <span className="field__hint">{hint}</span> : null}
      {error ? (
        <span className="field__error" role="alert">
          {error}
        </span>
      ) : null}
    </div>
  )
}
