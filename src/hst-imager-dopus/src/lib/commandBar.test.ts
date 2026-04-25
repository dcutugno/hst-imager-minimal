import { describe, expect, it } from 'vitest';
import { DEFAULT_COMMAND_BAR, commandBarToJson, parseCommandBarConfig } from './commandBar';

describe('command bar config', () => {
  it('keeps delete and rename disabled in the default preset', () => {
    const disabled = DEFAULT_COMMAND_BAR.filter((button) => button.action === 'delete' || button.action === 'rename');
    expect(disabled).toHaveLength(2);
    expect(disabled.every((button) => button.enabled === false)).toBe(true);
  });

  it('round trips editable JSON config', () => {
    const json = commandBarToJson(DEFAULT_COMMAND_BAR);
    const parsed = parseCommandBarConfig(json);
    expect(parsed.map((button) => button.id)).toEqual(DEFAULT_COMMAND_BAR.map((button) => button.id));
  });
});
