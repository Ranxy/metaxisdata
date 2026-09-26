/** One page of a list RPC, normalized so callers do not repeat the shape. */
export interface ListPage<T> {
  items: T[];
  nextPageToken: string;
}

/**
 * Walks every page of a list RPC and returns the concatenated items.
 *
 * Every list RPC caps its page size, so a caller that renders "everything" has to
 * follow `next_page_token`: reading the first page and ignoring the token drops
 * rows silently, which is how the dashboard and several pickers used to lose
 * data. A token that does not advance also ends the walk, so a repeated token
 * cannot loop forever.
 */
export async function listAll<T>(
  fetchPage: (pageToken: string) => Promise<ListPage<T>>
): Promise<T[]> {
  const items: T[] = [];
  let pageToken = "";
  for (;;) {
    const page = await fetchPage(pageToken);
    items.push(...page.items);
    const nextPageToken = page.nextPageToken;
    if (!nextPageToken || nextPageToken === pageToken) {
      return items;
    }
    pageToken = nextPageToken;
  }
}
