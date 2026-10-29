import type { Note } from '../types/note';

// Fictional examples for the popup preview. These are not location-aware.
export const demoNotes: Note[] = [
  {
    id: 'quiet-courtyard',
    category: 'HIDDEN GEM',
    title: 'A quiet courtyard café',
    description:
      'Look for the little archway beside the bookshop. The courtyard tables stay calm even when the main street gets busy.',
    postedAt: '2 hours ago',
    helpfulVotes: 28,
  },
  {
    id: 'market-lane',
    category: 'SHORTCUT',
    title: 'Skip the market bottleneck',
    description:
      'The covered lane one block east is open to pedestrians and saves a few minutes during the lunch rush.',
    postedAt: 'Yesterday',
    helpfulVotes: 16,
  },
  {
    id: 'bakery-timing',
    category: 'LOCAL TIP',
    title: 'Ask for the warm batch',
    description:
      'The bakery usually brings out fresh cardamom buns around 4 pm. They sell out quickly, so ask at the counter.',
    postedAt: '2 days ago',
    helpfulVotes: 41,
  },
  {
    id: 'parking-evening',
    category: 'PARKING',
    title: 'Evening parking on the side street',
    description:
      'Metered spots on the quieter side street tend to open up after 6 pm. Check the signs before leaving your car.',
    postedAt: '3 days ago',
    helpfulVotes: 12,
  },
  {
    id: 'rooftop-view',
    category: 'HIDDEN GEM',
    title: 'A rooftop view without the queue',
    description:
      'The public terrace above the arts center has a lovely sunset view. Use the lift near the west entrance.',
    postedAt: '4 days ago',
    helpfulVotes: 35,
  },
];
