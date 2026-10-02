import type { IdenticonType } from 'types/views/address';

export const IDENTICONS: Array<{ label: string; id: IdenticonType; sampleBg: string }> = [
  {
    label: 'GitHub',
    id: 'github',
    sampleBg: 'url("/explorer/static/identicon_logos/github.png") center / contain no-repeat',
  },
  {
    label: 'Metamask jazzicon',
    id: 'jazzicon',
    sampleBg: 'url("/explorer/static/identicon_logos/jazzicon.png") center / contain no-repeat',
  },
  {
    label: 'Ethereum blockies',
    id: 'blockie',
    sampleBg: 'url("/explorer/static/identicon_logos/blockies.png") center / contain no-repeat',
  },
  {
    label: 'Gradient avatar',
    id: 'gradient_avatar',
    sampleBg: 'url("/explorer/static/identicon_logos/gradient_avatar.png") center / contain no-repeat',
  },
  {
    label: 'Nouns',
    id: 'nouns',
    sampleBg: 'url("/explorer/static/identicon_logos/nouns.svg") center / contain no-repeat',
  },
];
