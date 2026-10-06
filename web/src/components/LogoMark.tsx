import { useId } from 'react'

/** The S3 Freeze snowflake mark (same artwork as public/favicon.svg). */
export function LogoMark({ className }: { className?: string }) {
  const id = useId().replace(/:/g, '')
  return (
    <svg viewBox="0 0 64 64" className={className} aria-hidden="true">
      <defs>
        <linearGradient id={`${id}ice`} x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#7dd3fc" />
          <stop offset=".5" stopColor="#0ea5e9" />
          <stop offset="1" stopColor="#2563eb" />
        </linearGradient>
        <radialGradient id={`${id}shine`} cx=".25" cy=".15" r=".7">
          <stop offset="0" stopColor="#fff" stopOpacity=".45" />
          <stop offset="1" stopColor="#fff" stopOpacity="0" />
        </radialGradient>
      </defs>
      <rect width="64" height="64" rx="16" fill={`url(#${id}ice)`} />
      <rect width="64" height="64" rx="16" fill={`url(#${id}shine)`} />
      <g fill="none" stroke="#fff" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round">
        {[0, 60, 120, 180, 240, 300].map((deg) => (
          <path
            key={deg}
            transform={`rotate(${deg} 32 32)`}
            d="M32 32V12.5M32 19.5l-5.2-5.2M32 19.5l5.2-5.2M32 26l-3.6-3.6M32 26l3.6-3.6"
          />
        ))}
      </g>
      <circle cx="32" cy="32" r="3" fill="#fff" />
    </svg>
  )
}
