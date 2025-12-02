import { useState, useCallback } from 'react';
import { useWebSocket } from './useWebSocket';

interface LogLine {
  timestamp: string;
  message: string;
}

interface UseLogsOptions {
  tail?: number;
  follow?: boolean;
  maxLines?: number;
}

export function useLogs(containerId: string | null, options: UseLogsOptions = {}) {
  const { tail = 100, follow = true, maxLines = 1000 } = options;
  const [logs, setLogs] = useState<LogLine[]>([]);

  const handleMessage = useCallback((data: string) => {
    // Parse log line (format: timestamp message)
    const parts = data.split(' ');
    const timestamp = parts[0] || '';
    const message = parts.slice(1).join(' ');

    setLogs(prev => {
      const newLogs = [...prev, { timestamp, message }];
      // Keep only the last maxLines
      if (newLogs.length > maxLines) {
        return newLogs.slice(-maxLines);
      }
      return newLogs;
    });
  }, [maxLines]);

  const url = containerId
    ? `/ws/containers/${containerId}/logs?tail=${tail}&follow=${follow}`
    : null;

  const { isConnected } = useWebSocket(url, {
    onMessage: handleMessage,
    onConnect: () => setLogs([]),
  });

  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  return { logs, isConnected, clearLogs };
}
