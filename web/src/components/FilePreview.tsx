import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  ArchiveRestore,
  ChevronLeft,
  ChevronRight,
  Download,
  File,
  FileArchive,
  FileAudio,
  FileCode,
  FileImage,
  FileText,
  FileVideo,
  Loader2,
  type LucideIcon,
} from 'lucide-react'
import type { Listing } from '@/lib/api'
import { cn, formatBytes, formatDate } from '@/lib/utils'
import { Button, buttonVariants, Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui'

export type FileItem = Listing['files'][number]
export type FileKind = 'image' | 'video' | 'audio' | 'text' | 'archive' | 'other'

const ext = (name: string) => name.slice(name.lastIndexOf('.') + 1).toLowerCase()

const IMAGE = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'avif', 'bmp', 'svg', 'ico']
const VIDEO = ['mp4', 'webm', 'mov', 'm4v', 'ogv']
const AUDIO = ['mp3', 'wav', 'ogg', 'oga', 'm4a', 'flac', 'aac', 'opus']
const TEXT = [
  'txt', 'md', 'markdown', 'json', 'csv', 'tsv', 'log', 'yml', 'yaml', 'xml', 'html', 'htm', 'css', 'js', 'mjs', 'ts', 'tsx',
  'jsx', 'go', 'py', 'rb', 'rs', 'java', 'c', 'h', 'cpp', 'cs', 'php', 'sh', 'bash', 'ps1', 'sql', 'ini', 'conf', 'cfg',
  'toml', 'env', 'properties', 'gitignore', 'dockerfile', 'tf', 'vue', 'svelte',
]
const ARCHIVE = ['zip', 'tar', 'gz', 'tgz', 'bz2', 'xz', 'zst', '7z', 'rar']

export function fileKind(f: { name: string; content_type?: string }): FileKind {
  const ct = f.content_type ?? ''
  const e = ext(f.name)
  if (ct.startsWith('image/') || IMAGE.includes(e)) return 'image'
  if (ct.startsWith('video/') || VIDEO.includes(e)) return 'video'
  if (ct.startsWith('audio/') || AUDIO.includes(e)) return 'audio'
  if (ct.startsWith('text/') || ct.includes('json') || ct.includes('xml') || TEXT.includes(e)) return 'text'
  if (ARCHIVE.includes(e)) return 'archive'
  return 'other'
}

const kindIcon: Record<FileKind, LucideIcon> = {
  image: FileImage,
  video: FileVideo,
  audio: FileAudio,
  text: FileText,
  archive: FileArchive,
  other: File,
}

export function FileIcon({ file, className }: { file: { name: string; content_type?: string }; className?: string }) {
  const kind = fileKind(file)
  const Icon = kind === 'text' && /\.(js|ts|tsx|jsx|go|py|rs|java|c|cpp|cs|php|sh|sql)$/i.test(file.name) ? FileCode : kindIcon[kind]
  const color = {
    image: 'text-pink-500',
    video: 'text-violet-500',
    audio: 'text-amber-500',
    text: 'text-sky-500',
    archive: 'text-orange-500',
    other: 'text-muted-foreground',
  }[kind]
  return <Icon className={cn('size-4 shrink-0', color, className)} />
}

export function fileUrl(jobId: string | number, snapId: string, path: string, mode?: 'download' | 'text') {
  const q = new URLSearchParams({ path })
  if (mode) q.set(mode, '1')
  return `/api/jobs/${jobId}/snapshots/${encodeURIComponent(snapId)}/file?${q}`
}

export function FilePreview({
  jobId,
  snapId,
  files,
  index,
  onIndex,
  onClose,
  onRestore,
}: {
  jobId: string | number
  snapId: string
  files: FileItem[]
  index: number
  onIndex: (i: number) => void
  onClose: () => void
  onRestore: (path: string) => void
}) {
  const file = files[index]
  const kind = fileKind(file)
  const prev = () => onIndex((index - 1 + files.length) % files.length)
  const next = () => onIndex((index + 1) % files.length)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft') prev()
      if (e.key === 'ArrowRight') next()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-w-4xl gap-3 p-4 sm:p-5">
        <div className="flex min-w-0 items-center gap-2 pr-8">
          <FileIcon file={file} />
          <div className="min-w-0">
            <DialogTitle className="truncate text-base">{file.name}</DialogTitle>
            <DialogDescription className="truncate text-xs">
              {formatBytes(file.size)} · {formatDate(file.modified)} · <span className="font-mono">{file.path}</span>
            </DialogDescription>
          </div>
        </div>

        <div className="relative flex min-h-64 items-center justify-center overflow-hidden rounded-lg bg-muted/60">
          <PreviewBody key={file.path} jobId={jobId} snapId={snapId} file={file} kind={kind} />
          {files.length > 1 && (
            <>
              <button
                onClick={prev}
                className="absolute left-2 top-1/2 -translate-y-1/2 rounded-full bg-card/90 p-2 shadow-md backdrop-blur hover:bg-card"
                title="Previous (←)"
              >
                <ChevronLeft className="size-5" />
              </button>
              <button
                onClick={next}
                className="absolute right-2 top-1/2 -translate-y-1/2 rounded-full bg-card/90 p-2 shadow-md backdrop-blur hover:bg-card"
                title="Next (→)"
              >
                <ChevronRight className="size-5" />
              </button>
            </>
          )}
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-xs text-muted-foreground tabular-nums">
            {index + 1} / {files.length}
          </span>
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => onRestore(file.path)}>
              <ArchiveRestore /> Restore
            </Button>
            <a href={fileUrl(jobId, snapId, file.path, 'download')} className={buttonVariants({ size: 'sm' })}>
              <Download /> Download
            </a>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function PreviewBody({ jobId, snapId, file, kind }: { jobId: string | number; snapId: string; file: FileItem; kind: FileKind }) {
  const [loaded, setLoaded] = useState(false)
  const [failed, setFailed] = useState(false)
  const url = fileUrl(jobId, snapId, file.path)

  if (failed) return <NoPreview file={file} text="This file could not be loaded." />

  switch (kind) {
    case 'image':
      return (
        <>
          {!loaded && <Loader2 className="absolute size-6 animate-spin text-muted-foreground" />}
          <img
            src={url}
            alt={file.name}
            onLoad={() => setLoaded(true)}
            onError={() => setFailed(true)}
            className={cn('max-h-[70vh] max-w-full object-contain transition-opacity', loaded ? 'opacity-100' : 'opacity-0')}
          />
        </>
      )
    case 'video':
      return <video src={url} controls autoPlay className="max-h-[70vh] max-w-full" onError={() => setFailed(true)} />
    case 'audio':
      return (
        <div className="flex w-full flex-col items-center gap-4 p-10">
          <FileAudio className="size-12 text-amber-500" />
          <audio src={url} controls autoPlay className="w-full max-w-md" onError={() => setFailed(true)} />
        </div>
      )
    case 'text':
      return <TextPreview url={fileUrl(jobId, snapId, file.path, 'text')} />
    default:
      return <NoPreview file={file} text="No preview for this file type." />
  }
}

function TextPreview({ url }: { url: string }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ['preview-text', url],
    queryFn: async () => {
      const res = await fetch(url, { credentials: 'same-origin' })
      if (!res.ok) throw new Error(res.statusText)
      return { text: await res.text(), truncated: res.headers.get('X-Truncated') === 'true' }
    },
    staleTime: Infinity,
  })
  if (isLoading) return <Loader2 className="size-6 animate-spin text-muted-foreground" />
  if (error || !data) return <p className="text-sm text-destructive">Could not load the file.</p>
  return (
    <div className="h-[70vh] w-full overflow-auto">
      <pre className="whitespace-pre-wrap break-words p-4 font-mono text-xs leading-relaxed">{data.text}</pre>
      {data.truncated && <p className="px-4 pb-4 text-xs text-muted-foreground">Preview limited to the first 256 KB — download for the full file.</p>}
    </div>
  )
}

function NoPreview({ file, text }: { file: FileItem; text: string }) {
  return (
    <div className="flex flex-col items-center gap-3 p-12 text-center">
      <FileIcon file={file} className="size-12" />
      <p className="text-sm text-muted-foreground">{text}</p>
    </div>
  )
}
