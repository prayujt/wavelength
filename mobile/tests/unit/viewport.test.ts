import { describe, expect, it } from 'vitest';
import { appViewportHeight } from '../../src/core/viewport';

const phone = {
  iphoneStandalone: true,
  width: 402,
  height: 812,
  screenWidth: 402,
  screenHeight: 874,
  top: 62,
  bottom: 34,
};
describe('installed iPhone screen geometry', () => {
  it('recovers the status-bar-sized band missing from the CSS viewport', () => {
    expect(appViewportHeight(phone)).toBe(874);
  });
  it('scales the correction with the layout width when the UI is zoomed', () => {
    expect(appViewportHeight({ ...phone, width: 352, height: 711 })).toBe(765);
  });
  it('leaves a correct full-screen viewport alone', () => {
    expect(appViewportHeight({ ...phone, height: 874 })).toBe(874);
  });
  it('does not stretch browser tabs, iPad windows, or large keyboard reductions', () => {
    expect(appViewportHeight({ ...phone, iphoneStandalone: false })).toBe(812);
    expect(appViewportHeight({ ...phone, height: 500 })).toBe(500);
    expect(appViewportHeight({ ...phone, width: 250 })).toBe(812);
  });
  it('does not add another inset to an oversized viewport', () => {
    expect(appViewportHeight({ ...phone, height: 900 })).toBe(900);
  });
  it('uses the screen aspect ratio after rotation without assuming portrait screen APIs', () => {
    expect(appViewportHeight({ ...phone, width: 874, height: 381, top: 0, bottom: 21 })).toBe(402);
  });
});
