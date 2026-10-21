import { notFound } from 'next/navigation';
import TerminalModule from '@/page-modules/terminal';
import { parseTerminalSlug } from '@/widgets/terminal/sub-nav';

interface Props { params: Promise<{ slug: string }>; }

export default async function TerminalSlugPage({ params }: Props) {
  const { slug } = await params;
  const target = parseTerminalSlug(slug);
  if (target.kind === 'invalid') notFound();
  return <TerminalModule slug={target.kind === 'pool' ? target.pool : target.view} />;
}
