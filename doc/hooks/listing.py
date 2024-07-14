import logging
from os.path import normpath

from mkdocs.structure.pages import Page
from mkdocs.utils.templates import TemplateContext
from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.nav import Navigation

log = logging.getLogger("mkdocs.plugins.listing")


# Given a page with meta:
#
# listing:
#   - foo.md
#   - bar.md
#
# Will add a listing attribute to that page
# with the pages for foo.md and bar.md.
#
# Use with template: listing.html.


def on_page_context(
    ctx: TemplateContext,
    page: Page,
    config: MkDocsConfig,
    nav: Navigation,
) -> TemplateContext:
    # cur_page = page
    children = page.meta.get('listing', [])
    if not children:
        return ctx

    # foo/bar/baz.md -> foo/bar
    uri = page.file.src_uri
    dir = '/'.join(uri.split('/')[:-1])

    want_uris = [normpath(f'{dir}/{child}') for child in children]
    got_pages = [None] * len(want_uris)

    # mapping from requested page URI
    # to index of that page in got_pages.
    # Will be used to fill in got_pages
    # as we iterate over all pages.
    page_idxes = {
        uri: idx for idx, uri in enumerate(want_uris)
    }

    for file in ctx['pages']:
        uri = file.src_uri
        idx = page_idxes.get(uri)
        if idx is not None:
            got_pages[idx] = file.page

    # If there are any missing pages, fail.
    result = []
    for idx, page in enumerate(got_pages):
        if page is None:
            log.warn(f'page {want_uris[idx]} not found')
        result.append(page)

    ctx['page_listing'] = result
    return ctx
