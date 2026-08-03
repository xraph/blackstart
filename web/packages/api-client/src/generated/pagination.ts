// Pagination helpers

export interface PaginationParams {
  limit?: number;
  offset?: number;
  cursor?: string;
  page?: number;
  pageSize?: number;
}

export interface PaginatedResponse<T> {
  data: T[];
  nextCursor?: string;
  prevCursor?: string;
  hasMore?: boolean;
  total?: number;
  page?: number;
  pageSize?: number;
}

/**
 * Creates an async iterator for paginated endpoints
 */
export async function* paginateAll<T>(
  fetchPage: (params: PaginationParams) => Promise<PaginatedResponse<T>>,
  initialParams: PaginationParams = {}
): AsyncGenerator<T, void, undefined> {
  let params = { ...initialParams };
  let hasMore = true;

  while (hasMore) {
    const response = await fetchPage(params);

    // Yield all items from current page
    for (const item of response.data) {
      yield item;
    }

    // Check if there are more pages
    hasMore = response.hasMore || false;

    // Update pagination params based on response
    if (response.nextCursor) {
      params.cursor = response.nextCursor;
    } else if (params.page !== undefined) {
      params.page++;
    } else if (params.offset !== undefined && params.limit) {
      params.offset += params.limit;
    } else {
      hasMore = false;
    }
  }
}

/**
 * Collects all items from a paginated endpoint into an array
 */
export async function collectAll<T>(
  fetchPage: (params: PaginationParams) => Promise<PaginatedResponse<T>>,
  initialParams: PaginationParams = {},
  maxItems?: number
): Promise<T[]> {
  const items: T[] = [];
  let count = 0;

  for await (const item of paginateAll(fetchPage, initialParams)) {
    items.push(item);
    count++;

    if (maxItems && count >= maxItems) {
      break;
    }
  }

  return items;
}
