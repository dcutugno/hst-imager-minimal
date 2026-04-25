import type { CommandButton } from './models';

export const DEFAULT_COMMAND_BAR: CommandButton[] = [
  { id: 'copy', label: 'Copy', action: 'copy', enabled: true, hint: 'Copy selected entries to the opposite pane using fs copy' },
  { id: 'extract', label: 'Extract', action: 'extract', enabled: true, hint: 'Extract selected archive/image entry using fs extract' },
  { id: 'mkdir', label: 'Mkdir', action: 'mkdir', enabled: true, hint: 'Create directory using fs mkdir' },
  { id: 'info', label: 'Info', action: 'info', enabled: true, hint: 'Show hst-imager info for the active path' },
  { id: 'delete', label: 'Delete', action: 'delete', enabled: false, hint: 'Disabled by default; hst-imager-go supports local fs delete' },
  { id: 'rename', label: 'Rename', action: 'rename', enabled: false, hint: 'Disabled by default; hst-imager-go supports local fs rename' },
  { id: 'refresh', label: 'Reread', action: 'refresh', enabled: true, hint: 'Reload active pane' },
  { id: 'swap', label: 'Swap', action: 'swap', enabled: true, hint: 'Swap source and destination panes' },
  { id: 'config', label: 'Buttons', action: 'config', enabled: true, hint: 'Edit command bank JSON' }
];

export function parseCommandBarConfig(input: string): CommandButton[] {
  const parsed = JSON.parse(input) as unknown;
  if (!Array.isArray(parsed)) throw new Error('Command bar config must be a JSON array.');

  return parsed.map((item, index) => {
    if (!item || typeof item !== 'object') {
      throw new Error(`Command button ${index + 1} must be an object.`);
    }
    const button = item as Partial<CommandButton>;
    if (!button.id || !button.label || !button.action) {
      throw new Error(`Command button ${index + 1} needs id, label, and action.`);
    }
    return {
      id: String(button.id),
      label: String(button.label),
      action: button.action,
      enabled: button.enabled !== false,
      hint: button.hint ? String(button.hint) : String(button.label)
    } as CommandButton;
  });
}

export function commandBarToJson(buttons: CommandButton[]): string {
  return JSON.stringify(buttons, null, 2);
}
