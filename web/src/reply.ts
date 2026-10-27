/* Reply buttons on comments: put `@handle ` at the caret in the comment box.
   Progressive enhancement only; without JavaScript the button does nothing. */

function insertMention(field: HTMLTextAreaElement, handle: string): void {
  const mention = `@${handle} `
  const start = field.selectionStart ?? field.value.length
  const end = field.selectionEnd ?? start
  const before = field.value.slice(0, start)
  const after = field.value.slice(end)
  // A space only when the caret already follows a word.
  const lead = before && !/\s$/.test(before) ? ' ' : ''
  const insert = lead + mention

  field.value = before + insert + after
  const caret = before.length + insert.length
  field.focus()
  field.setSelectionRange(caret, caret)
}

/** Wire up every `[data-reply-to]` button on the page. Safe when none exists. */
export function initReply(): void {
  document.addEventListener('click', (event) => {
    const button = (event.target as HTMLElement | null)?.closest<HTMLElement>(
      '[data-reply-to]',
    )
    if (!button) return
    const handle = (button.dataset.replyTo || '').trim()
    const field = document.getElementById('comment-body')
    if (!handle || !(field instanceof HTMLTextAreaElement)) return

    event.preventDefault()
    insertMention(field, handle)
  })
}
